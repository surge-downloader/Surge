package events

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/surge-downloader/surge/internal/engine/types"
)

// ProgressMsg represents a progress update from the downloader
type ProgressMsg struct {
	DownloadID        string
	Downloaded        int64
	Total             int64
	Speed             float64 // bytes per second
	Elapsed           time.Duration
	ActiveConnections int
	ChunkBitmap       []byte
	BitmapWidth       int
	ActualChunkSize   int64
	ChunkProgress     []int64
}

// DownloadCompleteMsg signals that the download finished successfully
type DownloadCompleteMsg struct {
	DownloadID string
	Filename   string
	Elapsed    time.Duration
	Total      int64
}

// DownloadErrorMsg signals that an error occurred
type DownloadErrorMsg struct {
	DownloadID string
	Filename   string
	Err        error
}

func (m DownloadErrorMsg) MarshalJSON() ([]byte, error) {
	type encoded struct {
		DownloadID string `json:"DownloadID"`
		Filename   string `json:"Filename,omitempty"`
		Err        string `json:"Err,omitempty"`
	}

	out := encoded{
		DownloadID: m.DownloadID,
		Filename:   m.Filename,
	}
	if m.Err != nil {
		out.Err = m.Err.Error()
	}

	return json.Marshal(out)
}

func (m *DownloadErrorMsg) UnmarshalJSON(data []byte) error {
	var aux struct {
		DownloadID string          `json:"DownloadID"`
		Filename   string          `json:"Filename"`
		Err        json.RawMessage `json:"Err"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	m.DownloadID = aux.DownloadID
	m.Filename = aux.Filename
	m.Err = nil

	if len(aux.Err) == 0 {
		return nil
	}

	// Most common case: server sends Err as a string.
	var errStr string
	if err := json.Unmarshal(aux.Err, &errStr); err == nil {
		if errStr != "" {
			m.Err = errors.New(errStr)
		}
		return nil
	}

	// Backward/forward compatibility: accept non-string payloads (e.g. {}).
	raw := string(aux.Err)
	if raw != "" && raw != "null" {
		m.Err = errors.New(raw)
	}
	return nil
}

// DownloadStartedMsg is sent when a download actually starts (after metadata fetch)
type DownloadStartedMsg struct {
	DownloadID string
	URL        string
	Filename   string
	Total      int64
	DestPath   string               // Full path to the destination file
	State      *types.ProgressState `json:"-"`
}

type DownloadPausedMsg struct {
	DownloadID string
	Filename   string
	Downloaded int64
}

type DownloadResumedMsg struct {
	DownloadID string
	Filename   string
}

type DownloadQueuedMsg struct {
	DownloadID string
	Filename   string
}

type DownloadRemovedMsg struct {
	DownloadID string
	Filename   string
}

// DownloadRequestMsg signals a request to start a download (e.g. from extension)
// that may need user confirmation or duplicate checking
type DownloadRequestMsg struct {
	ID       string
	URL      string
	Filename string
	Path     string
	Mirrors  []string
	Headers  map[string]string
}
