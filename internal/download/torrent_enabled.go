package download

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/surge-downloader/surge/internal/engine/events"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/source"
	"github.com/surge-downloader/surge/internal/torrent"
	"github.com/surge-downloader/surge/internal/utils"
)

func TorrentDownload(ctx context.Context, cfg *types.DownloadConfig) error {
	if cfg == nil {
		return fmt.Errorf("nil config")
	}
	if source.IsMagnet(cfg.URL) {
		return fmt.Errorf("magnet links are not supported yet")
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
		cfg.State.SetCancelFunc(cancel)
	}

	placeholderName := cfg.Filename
	if placeholderName == "" {
		if parsed, err := url.Parse(cfg.URL); err == nil && parsed.Path != "" {
			base := path.Base(parsed.Path)
			if base != "" && base != "/" && base != "." {
				placeholderName = base
			}
		}
		if placeholderName == "" {
			placeholderName = "torrent"
		}
	}
	placeholderDest := filepath.Join(outPath, placeholderName)
	cfg.Filename = placeholderName
	cfg.DestPath = placeholderDest
	if cfg.State != nil {
		cfg.State.SetFilename(placeholderName)
		cfg.State.SetDestPath(placeholderDest)
	}
	if cfg.ProgressCh != nil {
		cfg.ProgressCh <- events.DownloadStartedMsg{
			DownloadID: cfg.ID,
			URL:        cfg.URL,
			Filename:   placeholderName,
			Total:      0,
			DestPath:   placeholderDest,
			State:      cfg.State,
		}
	}

	var meta *torrent.TorrentMeta
	m, err := torrent.FetchTorrent(downloadCtx, cfg.URL, cfg.Headers)
	if err != nil {
		return err
	}
	meta = m

	name := meta.Info.Name
	if name == "" {
		name = "torrent"
	}
	destPath := filepath.Join(outPath, name)
	cfg.Filename = name
	cfg.DestPath = destPath
	if cfg.State != nil {
		cfg.State.DestPath = destPath
		cfg.State.SetTotalSize(meta.Info.TotalLength())
	}

	if cfg.ProgressCh != nil {
		cfg.ProgressCh <- events.DownloadStartedMsg{
			DownloadID: cfg.ID,
			URL:        cfg.URL,
			Filename:   name,
			Total:      meta.Info.TotalLength(),
			DestPath:   destPath,
			State:      cfg.State,
		}
	}

	runtime := cfg.Runtime
	if runtime == nil {
		runtime = &types.RuntimeConfig{}
	}

	runner, err := torrent.NewRunner(meta, outPath, torrent.SessionConfig{
		ListenAddr:      fmt.Sprintf("0.0.0.0:%d", runtime.GetTorrentListenPort()),
		BootstrapNodes:  []string{"router.bittorrent.com:6881", "dht.transmissionbt.com:6881"},
		TotalLength:     meta.Info.TotalLength(),
		MaxPeers:        runtime.GetTorrentMaxConnections(),
		UploadSlots:     runtime.GetTorrentUploadSlots(),
		RequestPipeline: runtime.GetTorrentRequestPipeline(),
	}, cfg.State)
	if err != nil {
		return err
	}

	start := time.Now()
	runner.Start(downloadCtx)

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-downloadCtx.Done():
			if cfg.State != nil && !cfg.State.Done.Load() && (cfg.State.IsPaused() || cfg.State.IsPausing() || errors.Is(downloadCtx.Err(), context.Canceled)) {
				persistTorrentEntry(cfg, destPath, name, meta.Info.TotalLength(), start, "paused")
				persistTorrentPauseState(cfg, destPath, name, meta.Info.TotalLength(), start)
				return types.ErrPaused
			}
			if errors.Is(downloadCtx.Err(), context.Canceled) {
				return nil
			}
			persistTorrentEntry(cfg, destPath, name, meta.Info.TotalLength(), start, "error")
			return downloadCtx.Err()
		case <-ticker.C:
			if cfg.State != nil {
				cfg.State.ActiveWorkers.Store(int32(runner.ActivePeerCount()))
				ps := runner.PeerStats()
				cfg.State.SetTorrentPeerCounters(types.TorrentPeerCounters{
					Discovered:      ps.Discovered,
					Pending:         ps.Pending,
					Active:          ps.Active,
					DialAttempts:    ps.DialAttempts,
					DialSuccess:     ps.DialSuccess,
					DialFailures:    ps.DialFailures,
					InboundAccepted: ps.InboundAccepted,
				})
				downloaded := cfg.State.VerifiedProgress.Load()
				if downloaded >= meta.Info.TotalLength() {
					persistTorrentEntry(cfg, destPath, name, meta.Info.TotalLength(), start, "completed")
					if cfg.ProgressCh != nil {
						elapsed := time.Since(start)
						if savedElapsed := cfg.State.GetSavedElapsed(); savedElapsed > 0 {
							elapsed += savedElapsed
						}
						avgSpeed := float64(0)
						if elapsed.Seconds() > 0 {
							avgSpeed = float64(meta.Info.TotalLength()) / elapsed.Seconds()
						}
						cfg.ProgressCh <- events.DownloadCompleteMsg{
							DownloadID: cfg.ID,
							Filename:   name,
							Elapsed:    elapsed,
							Total:      meta.Info.TotalLength(),
							AvgSpeed:   avgSpeed,
						}
					}
					return nil
				}
			}
		}
	}
}

func persistTorrentPauseState(cfg *types.DownloadConfig, destPath, name string, total int64, start time.Time) {
	if cfg == nil || cfg.State == nil {
		return
	}
	elapsed := time.Since(start)
	if savedElapsed := cfg.State.GetSavedElapsed(); savedElapsed > 0 {
		elapsed += savedElapsed
	}
	paused := &types.DownloadState{
		ID:         cfg.ID,
		URL:        cfg.URL,
		DestPath:   destPath,
		Filename:   name,
		TotalSize:  total,
		Downloaded: cfg.State.VerifiedProgress.Load(),
		PausedAt:   time.Now().Unix(),
		Elapsed:    elapsed.Nanoseconds(),
		Mirrors:    cfg.Mirrors,
	}
	if err := state.SaveState(cfg.URL, destPath, paused); err != nil {
		utils.Debug("Torrent: failed to persist pause state: %v", err)
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
