//go:build torrent

package download

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"

	"github.com/surge-downloader/surge/internal/engine/events"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/source"
	"github.com/surge-downloader/surge/internal/utils"
)

func TorrentDownload(ctx context.Context, cfg *types.DownloadConfig) error {
	if cfg == nil {
		return fmt.Errorf("nil config")
	}

	outPath := cfg.OutputPath
	if outPath == "" {
		outPath = "."
	}
	outPath = utils.EnsureAbsPath(outPath)
	if err := os.MkdirAll(outPath, 0o755); err != nil {
		return fmt.Errorf("failed to create output dir: %w", err)
	}

	downloadCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if cfg.State != nil {
		cfg.State.CancelFunc = cancel
	}

	tcfg := torrent.NewDefaultClientConfig()
	tcfg.DataDir = outPath

	client, err := torrent.NewClient(tcfg)
	if err != nil {
		return fmt.Errorf("torrent client init failed: %w", err)
	}
	defer client.Close()

	var t *torrent.Torrent
	if source.IsMagnet(cfg.URL) {
		t, err = client.AddMagnet(cfg.URL)
		if err != nil {
			return fmt.Errorf("add magnet failed: %w", err)
		}
	} else {
		mi, miErr := fetchTorrentMeta(downloadCtx, cfg.URL, cfg.Headers)
		if miErr != nil {
			return miErr
		}
		t, err = client.AddTorrent(mi)
		if err != nil {
			return fmt.Errorf("add torrent failed: %w", err)
		}
	}
	defer t.Drop()

	<-t.GotInfo()

	info := t.Info()
	if info == nil {
		return fmt.Errorf("torrent metadata unavailable")
	}

	total := t.Length()
	if total <= 0 {
		total = info.TotalLength()
	}
	if total <= 0 {
		return fmt.Errorf("torrent total size unknown")
	}

	name := t.Name()
	if name == "" {
		name = info.Name
	}

	destPath := filepath.Join(outPath, name)
	cfg.Filename = name
	cfg.DestPath = destPath
	if cfg.State != nil {
		cfg.State.DestPath = destPath
		cfg.State.SetTotalSize(total)
	}

	if cfg.ProgressCh != nil {
		cfg.ProgressCh <- events.DownloadStartedMsg{
			DownloadID: cfg.ID,
			URL:        cfg.URL,
			Filename:   name,
			Total:      total,
			DestPath:   destPath,
			State:      cfg.State,
		}
	}

	t.DownloadAll()

	start := time.Now()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-downloadCtx.Done():
			if cfg.State != nil && (cfg.State.IsPaused() || cfg.State.IsPausing()) {
				persistTorrentEntry(cfg, destPath, name, total, start, "paused")
				return types.ErrPaused
			}
			persistTorrentEntry(cfg, destPath, name, total, start, "error")
			return downloadCtx.Err()
		case <-ticker.C:
			completed := t.BytesCompleted()
			if cfg.State != nil {
				cfg.State.Downloaded.Store(completed)
				cfg.State.VerifiedProgress.Store(completed)
			}
			if completed >= total {
				persistTorrentEntry(cfg, destPath, name, total, start, "completed")
				if cfg.ProgressCh != nil {
					cfg.ProgressCh <- events.DownloadCompleteMsg{
						DownloadID: cfg.ID,
						Filename:   name,
						Elapsed:    time.Since(start),
						Total:      total,
					}
				}
				return nil
			}
		}
	}
}

func persistTorrentEntry(cfg *types.DownloadConfig, destPath, name string, total int64, start time.Time, status string) {
	if cfg == nil {
		return
	}
	downloaded := int64(0)
	if cfg.State != nil {
		downloaded = cfg.State.Downloaded.Load()
	}

	entry := types.DownloadEntry{
		ID:         cfg.ID,
		URL:        cfg.URL,
		DestPath:   destPath,
		Filename:   name,
		Status:     status,
		TotalSize:  total,
		Downloaded: downloaded,
	}

	switch status {
	case "completed":
		entry.CompletedAt = time.Now().Unix()
		entry.TimeTaken = time.Since(start).Milliseconds()
	}

	if err := state.AddToMasterList(entry); err != nil {
		utils.Debug("Torrent: failed to persist %s entry: %v", status, err)
	}
	if status == "error" && cfg.ProgressCh != nil {
		cfg.ProgressCh <- events.DownloadErrorMsg{
			DownloadID: cfg.ID,
			Filename:   name,
			Err:        fmt.Errorf("torrent download failed"),
		}
	}
}

func fetchTorrentMeta(ctx context.Context, url string, headers map[string]string) (*metainfo.MetaInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create torrent request failed: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("torrent fetch failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("torrent fetch error: %s - %s", resp.Status, string(bytes.TrimSpace(body)))
	}

	mi, err := metainfo.Load(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("parse torrent failed: %w", err)
	}
	return mi, nil
}
