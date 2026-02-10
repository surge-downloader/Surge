package core

import (
	"context"

	"github.com/surge-downloader/surge/internal/engine/types"
)

// DownloadService defines the interface for interacting with the download engine.
// This abstraction allows the TUI to switch between a local embedded backend
// and a remote daemon connection.
type DownloadService interface {
	// List returns the status of all active and completed downloads.
	List() ([]types.DownloadStatus, error)

	// History returns completed downloads
	History() ([]types.DownloadEntry, error)

	// Add queues a new download.
	Add(url string, path string, filename string, mirrors []string, headers map[string]string) (string, error)

	// Pause pauses an active download.
	Pause(id string) error

	// Resume resumes a paused download.
	Resume(id string) error

	// Delete cancels and removes a download.
	Delete(id string) error

	// StreamEvents returns a channel that receives real-time download events.
	// For local mode, this is a direct channel.
	// For remote mode, this is sourced from SSE.
	StreamEvents(ctx context.Context) (<-chan interface{}, func(), error)

	// Publish emits an event into the service's event stream.
	Publish(msg interface{}) error

	// GetStatus returns a status for a single download by id.
	GetStatus(id string) (*types.DownloadStatus, error)

	// Shutdown handles graceful shutdown of the service
	Shutdown() error
}
