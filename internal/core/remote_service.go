package core

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/surge-downloader/surge/internal/engine/events"
	"github.com/surge-downloader/surge/internal/engine/types"
)

// RemoteDownloadService implements DownloadService for a remote daemon.
type RemoteDownloadService struct {
	BaseURL   string
	Token     string
	Client    *http.Client
	SSEClient *http.Client
	ctx       context.Context
	cancel    context.CancelFunc
}

// NewRemoteDownloadService creates a new remote service instance.
func NewRemoteDownloadService(baseURL string, token string) *RemoteDownloadService {
	ctx, cancel := context.WithCancel(context.Background())
	return &RemoteDownloadService{
		BaseURL:   baseURL,
		Token:     token,
		Client:    &http.Client{Timeout: 30 * time.Second},
		SSEClient: &http.Client{},
		ctx:       ctx,
		cancel:    cancel,
	}
}

func (s *RemoteDownloadService) doRequest(method, path string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewBuffer(jsonBody)
	}

	req, err := http.NewRequestWithContext(s.ctx, method, s.BaseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+s.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		defer func() { _ = resp.Body.Close() }()
		// Limit error body read to 1KB to prevent DoS
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("API error %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return resp, nil
}

// List returns the status of all active and completed downloads.
func (s *RemoteDownloadService) List() ([]types.DownloadStatus, error) {
	resp, err := s.doRequest("GET", "/list", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var statuses []types.DownloadStatus
	if err := json.NewDecoder(resp.Body).Decode(&statuses); err != nil {
		return nil, err
	}
	return statuses, nil
}

// History returns completed downloads
func (s *RemoteDownloadService) History() ([]types.DownloadEntry, error) {
	resp, err := s.doRequest("GET", "/history", nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var history []types.DownloadEntry
	if err := json.NewDecoder(resp.Body).Decode(&history); err != nil {
		return nil, err
	}
	return history, nil
}

// GetStatus returns a status for a single download by id.
func (s *RemoteDownloadService) GetStatus(id string) (*types.DownloadStatus, error) {
	resp, err := s.doRequest("GET", "/download?id="+url.QueryEscape(id), nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	var status types.DownloadStatus
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		return nil, err
	}
	return &status, nil
}

// Add queues a new download.
func (s *RemoteDownloadService) Add(url string, path string, filename string, mirrors []string, headers map[string]string) (string, error) {
	req := map[string]interface{}{
		"url":           url,
		"path":          path,
		"filename":      filename,
		"mirrors":       mirrors,
		"headers":       headers,
		"skip_approval": true,
	}

	resp, err := s.doRequest("POST", "/download", req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result["id"], nil
}

// Pause pauses an active download.
func (s *RemoteDownloadService) Pause(id string) error {
	resp, err := s.doRequest("POST", "/pause?id="+url.QueryEscape(id), nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return nil
}

// Resume resumes a paused download.
func (s *RemoteDownloadService) Resume(id string) error {
	resp, err := s.doRequest("POST", "/resume?id="+url.QueryEscape(id), nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return nil
}

// Delete cancels and removes a download.
func (s *RemoteDownloadService) Delete(id string) error {
	resp, err := s.doRequest("POST", "/delete?id="+url.QueryEscape(id), nil)
	// Some APIs use DELETE method, checking previous implementation in server it supports both POST and DELETE
	// but mostly POST for actions. Let's stick to POST as per server implementation.
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	return nil
}

// Shutdown stops the service.
func (s *RemoteDownloadService) Shutdown() error {
	s.cancel()
	return nil
}

// StreamEvents returns a channel that receives real-time download events via SSE.
func (s *RemoteDownloadService) StreamEvents(ctx context.Context) (<-chan interface{}, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ch := make(chan interface{}, 100)
	go s.streamWithReconnect(ctx, ch)
	return ch, func() {}, nil
}

// Publish emits an event into the service's event stream.
// Remote services do not accept client-side event injection.
func (s *RemoteDownloadService) Publish(msg interface{}) error {
	return fmt.Errorf("publish not supported for remote service")
}

func (s *RemoteDownloadService) streamWithReconnect(ctx context.Context, ch chan interface{}) {
	defer close(ch)
	backoff := 1 * time.Second
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ctx.Done():
			return
		default:
		}

		err := s.connectSSE(ctx, ch)
		if err == nil {
			return // Clean shutdown (e.g. server closed stream cleanly or context canceled during request)
		}
		// Check context again before sleeping
		select {
		case <-s.ctx.Done():
			return
		case <-ctx.Done():
			return
		case <-time.After(backoff):
			// Continue
		}

		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (s *RemoteDownloadService) connectSSE(ctx context.Context, ch chan interface{}) error {
	req, err := http.NewRequestWithContext(ctx, "GET", s.BaseURL+"/events", nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")

	resp, err := s.SSEClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to connect to event stream: %s", resp.Status)
	}

	reader := bufio.NewReader(resp.Body)
	for {
		eventType := ""
		var dataLines []string

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return err
			}
			line = strings.TrimRight(line, "\r\n")

			// Blank line dispatches event
			if line == "" {
				break
			}
			// Comment/heartbeat
			if strings.HasPrefix(line, ":") {
				continue
			}
			if strings.HasPrefix(line, "event:") {
				eventType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
				continue
			}
			if strings.HasPrefix(line, "data:") {
				dataLines = append(dataLines, strings.TrimSpace(strings.TrimPrefix(line, "data:")))
				continue
			}
		}

		if eventType == "" || len(dataLines) == 0 {
			continue
		}
		jsonData := strings.Join(dataLines, "\n")

		var msg interface{}
		switch eventType {
		case "progress":
			var m events.ProgressMsg
			if err := json.Unmarshal([]byte(jsonData), &m); err != nil {
				continue
			}
			msg = m
		case "started":
			var m events.DownloadStartedMsg
			if err := json.Unmarshal([]byte(jsonData), &m); err != nil {
				continue
			}
			msg = m
		case "complete":
			var m events.DownloadCompleteMsg
			if err := json.Unmarshal([]byte(jsonData), &m); err != nil {
				continue
			}
			msg = m
		case "error":
			var m events.DownloadErrorMsg
			if err := json.Unmarshal([]byte(jsonData), &m); err != nil {
				continue
			}
			msg = m
		case "paused":
			var m events.DownloadPausedMsg
			if err := json.Unmarshal([]byte(jsonData), &m); err != nil {
				continue
			}
			msg = m
		case "resumed":
			var m events.DownloadResumedMsg
			if err := json.Unmarshal([]byte(jsonData), &m); err != nil {
				continue
			}
			msg = m
		case "queued":
			var m events.DownloadQueuedMsg
			if err := json.Unmarshal([]byte(jsonData), &m); err != nil {
				continue
			}
			msg = m
		case "removed":
			var m events.DownloadRemovedMsg
			if err := json.Unmarshal([]byte(jsonData), &m); err != nil {
				continue
			}
			msg = m
		case "request":
			var m events.DownloadRequestMsg
			if err := json.Unmarshal([]byte(jsonData), &m); err != nil {
				continue
			}
			msg = m
		default:
			continue
		}

		// Non-blocking send
		select {
		case ch <- msg:
		default:
			// Drop message if channel is full to prevent blocking the reader
		}
	}
}
