package core

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/surge-downloader/surge/internal/engine/events"
	"github.com/surge-downloader/surge/internal/engine/types"
)

// RemoteDownloadService implements DownloadService for a remote daemon.
type RemoteDownloadService struct {
	BaseURL string
	Token   string
	Client  *http.Client
}

// NewRemoteDownloadService creates a new remote service instance.
func NewRemoteDownloadService(baseURL string, token string) *RemoteDownloadService {
	return &RemoteDownloadService{
		BaseURL: baseURL,
		Token:   token,
		Client:  &http.Client{},
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

	req, err := http.NewRequest(method, s.BaseURL+path, bodyReader)
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
		defer resp.Body.Close()
		bodyBytes, _ := io.ReadAll(resp.Body)
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
	defer resp.Body.Close()

	var statuses []types.DownloadStatus
	if err := json.NewDecoder(resp.Body).Decode(&statuses); err != nil {
		return nil, err
	}
	return statuses, nil
}

// Add queues a new download.
func (s *RemoteDownloadService) Add(url string, path string, filename string, mirrors []string) (string, error) {
	req := map[string]interface{}{
		"url":      url,
		"path":     path,
		"filename": filename,
		"mirrors":  mirrors,
	}

	resp, err := s.doRequest("POST", "/download", req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result["id"], nil
}

// Pause pauses an active download.
func (s *RemoteDownloadService) Pause(id string) error {
	resp, err := s.doRequest("POST", "/pause?id="+id, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// Resume resumes a paused download.
func (s *RemoteDownloadService) Resume(id string) error {
	resp, err := s.doRequest("POST", "/resume?id="+id, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// Delete cancels and removes a download.
func (s *RemoteDownloadService) Delete(id string) error {
	resp, err := s.doRequest("POST", "/delete?id="+id, nil)
	// Some APIs use DELETE method, checking previous implementation in server it supports both POST and DELETE
	// but mostly POST for actions. Let's stick to POST as per server implementation.
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// Shutdown stops the service.
func (s *RemoteDownloadService) Shutdown() error {
	resp, err := s.doRequest("POST", "/shutdown", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// StreamEvents returns a channel that receives real-time download events via SSE.
func (s *RemoteDownloadService) StreamEvents() (<-chan interface{}, error) {
	req, err := http.NewRequest("GET", s.BaseURL+"/events", nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+s.Token)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")

	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		resp.Body.Close()
		return nil, fmt.Errorf("failed to connect to event stream: %s", resp.Status)
	}

	ch := make(chan interface{}, 100)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}

			// Simple SSE parser
			// Expecting:
			// event: <type>
			// data: <json>
			// \n

			if strings.HasPrefix(line, "event: ") {
				eventType := strings.TrimSpace(strings.TrimPrefix(line, "event: "))

				dataLine, err := reader.ReadString('\n')
				if err != nil {
					return
				}
				if !strings.HasPrefix(dataLine, "data: ") {
					continue
				}

				jsonData := strings.TrimSpace(strings.TrimPrefix(dataLine, "data: "))

				// Read empty line
				_, _ = reader.ReadString('\n')

				var msg interface{}

				switch eventType {
				case "progress":
					var m events.ProgressMsg
					_ = json.Unmarshal([]byte(jsonData), &m)
					msg = m
				case "started":
					var m events.DownloadStartedMsg
					_ = json.Unmarshal([]byte(jsonData), &m)
					msg = m
				case "complete":
					var m events.DownloadCompleteMsg
					_ = json.Unmarshal([]byte(jsonData), &m)
					msg = m
				case "error":
					var m events.DownloadErrorMsg
					_ = json.Unmarshal([]byte(jsonData), &m)
					msg = m
				case "paused":
					var m events.DownloadPausedMsg
					_ = json.Unmarshal([]byte(jsonData), &m)
					msg = m
				case "resumed":
					var m events.DownloadResumedMsg
					_ = json.Unmarshal([]byte(jsonData), &m)
					msg = m
				case "queued":
					var m events.DownloadQueuedMsg
					_ = json.Unmarshal([]byte(jsonData), &m)
					msg = m
				case "removed":
					var m events.DownloadRemovedMsg
					_ = json.Unmarshal([]byte(jsonData), &m)
					msg = m
				case "request":
					var m events.DownloadRequestMsg
					_ = json.Unmarshal([]byte(jsonData), &m)
					msg = m
				default:
					continue
				}

				if msg != nil {
					ch <- msg
				}
			}
		}
	}()

	return ch, nil
}
