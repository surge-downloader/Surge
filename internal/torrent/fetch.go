package torrent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

func FetchTorrent(ctx context.Context, url string, headers map[string]string) (*TorrentMeta, error) {
	client := &http.Client{
		Timeout: 20 * time.Second,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("torrent fetch error: %s - %s", resp.Status, string(bytes.TrimSpace(body)))
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return ParseTorrent(data)
}
