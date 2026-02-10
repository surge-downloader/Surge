package core

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/surge-downloader/surge/internal/config"
	"github.com/surge-downloader/surge/internal/download"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/utils"
)

// LocalDownloadService implements DownloadService for the local embedded engine.
type LocalDownloadService struct {
	Pool    *download.WorkerPool
	InputCh chan interface{}

	// Broadcast fields
	listeners  []chan interface{}
	listenerMu sync.Mutex
}

// NewLocalDownloadService creates a new specific service instance.
func NewLocalDownloadService(pool *download.WorkerPool, progressCh chan interface{}) *LocalDownloadService {
	s := &LocalDownloadService{
		Pool:      pool,
		InputCh:   progressCh,
		listeners: make([]chan interface{}, 0),
	}
	// Start broadcaster
	go s.broadcastLoop()
	return s
}

func (s *LocalDownloadService) broadcastLoop() {
	for msg := range s.InputCh {
		s.listenerMu.Lock()
		for _, ch := range s.listeners {
			// Non-blocking send to avoid stalling if a client is slow
			select {
			case ch <- msg:
			default:
				// Drop message if channel is full
			}
		}
		s.listenerMu.Unlock()
	}
	// Close all listeners when input closes
	s.listenerMu.Lock()
	for _, ch := range s.listeners {
		close(ch)
	}
	s.listeners = nil
	s.listenerMu.Unlock()
}

// StreamEvents returns a channel that receives real-time download events.
func (s *LocalDownloadService) StreamEvents() (<-chan interface{}, error) {
	ch := make(chan interface{}, 100)
	s.listenerMu.Lock()
	s.listeners = append(s.listeners, ch)
	s.listenerMu.Unlock()
	return ch, nil
}

// List returns the status of all active and completed downloads.
func (s *LocalDownloadService) List() ([]types.DownloadStatus, error) {
	var statuses []types.DownloadStatus

	// 1. Get active downloads from pool
	if s.Pool != nil {
		activeConfigs := s.Pool.GetAll()
		for _, cfg := range activeConfigs {
			status := types.DownloadStatus{
				ID:       cfg.ID,
				URL:      cfg.URL,
				Filename: cfg.Filename,
				Status:   "downloading",
			}

			if cfg.State != nil {
				status.TotalSize = cfg.State.TotalSize
				status.Downloaded = cfg.State.Downloaded.Load()
				if status.TotalSize > 0 {
					status.Progress = float64(status.Downloaded) * 100 / float64(status.TotalSize)
				}

				// Calculate speed from progress
				downloaded, _, _, sessionElapsed, connections, sessionStart := cfg.State.GetProgress()
				sessionDownloaded := downloaded - sessionStart
				if sessionElapsed.Seconds() > 0 && sessionDownloaded > 0 {
					status.Speed = float64(sessionDownloaded) / sessionElapsed.Seconds() / (1024 * 1024)

					// Calculate ETA (seconds remaining)
					remaining := status.TotalSize - status.Downloaded
					if remaining > 0 && status.Speed > 0 {
						speedBytes := status.Speed * 1024 * 1024
						status.ETA = int64(float64(remaining) / speedBytes)
					}
				}

				// Get active connections count
				status.Connections = int(connections)

				// Update status based on state
				if cfg.State.IsPaused() {
					status.Status = "paused"
				} else if cfg.State.Done.Load() {
					status.Status = "completed"
				}
			}

			statuses = append(statuses, status)
		}
	}

	// 2. Fetch from database for history/paused/completed
	dbDownloads, err := state.ListAllDownloads()
	if err == nil {
		// Create a map of existing IDs to avoid duplicates
		existingIDs := make(map[string]bool)
		for _, s := range statuses {
			existingIDs[s.ID] = true
		}

		for _, d := range dbDownloads {
			// Skip if already present (active)
			if existingIDs[d.ID] {
				continue
			}

			var progress float64
			if d.TotalSize > 0 {
				progress = float64(d.Downloaded) * 100 / float64(d.TotalSize)
			} else if d.Status == "completed" {
				progress = 100.0
			}

			// Calculate speed for completed items if data available
			var speed float64
			if d.Status == "completed" && d.TimeTaken > 0 {
				speed = float64(d.TotalSize) * 1000 / float64(d.TimeTaken) / (1024 * 1024)
			}

			statuses = append(statuses, types.DownloadStatus{
				ID:          d.ID,
				URL:         d.URL,
				Filename:    d.Filename,
				Status:      d.Status,
				TotalSize:   d.TotalSize,
				Downloaded:  d.Downloaded,
				Progress:    progress,
				Speed:       speed,
				Connections: 0,
			})
		}
	}

	return statuses, nil
}

// Add queues a new download.
func (s *LocalDownloadService) Add(url string, path string, filename string, mirrors []string) (string, error) {
	if s.Pool == nil {
		return "", fmt.Errorf("worker pool not initialized")
	}

	settings, err := config.LoadSettings()
	if err != nil {
		settings = config.DefaultSettings()
	}

	// Prepare output path
	outPath := path
	if outPath == "" {
		if settings.General.DefaultDownloadDir != "" {
			outPath = settings.General.DefaultDownloadDir
		} else {
			outPath = "."
		}
	}
	outPath = utils.EnsureAbsPath(outPath)

	id := uuid.New().String()

	// Create configuration
	cfg := types.DownloadConfig{
		URL:        url,
		Mirrors:    mirrors,
		OutputPath: outPath,
		ID:         id,
		Filename:   filename, // If empty, will be auto-detected
		Verbose:    false,
		ProgressCh: s.InputCh,
		State:      types.NewProgressState(id, 0),
		Runtime:    convertRuntimeConfig(settings.ToRuntimeConfig()),
	}

	s.Pool.Add(cfg)

	return id, nil
}

// Pause pauses an active download.
func (s *LocalDownloadService) Pause(id string) error {
	if s.Pool == nil {
		return fmt.Errorf("worker pool not initialized")
	}

	if s.Pool.Pause(id) {
		return nil
	}

	// If not in pool, check if it's already paused/stopped in DB
	entry, err := state.GetDownload(id)
	if err == nil && entry != nil {
		return nil // Already stopped
	}

	return fmt.Errorf("download not found")
}

// Resume resumes a paused download.
func (s *LocalDownloadService) Resume(id string) error {
	if s.Pool == nil {
		return fmt.Errorf("worker pool not initialized")
	}

	// Try pool resume first
	if s.Pool.Resume(id) {
		return nil
	}

	// Cold Resume Logic
	entry, err := state.GetDownload(id)
	if err != nil || entry == nil {
		return fmt.Errorf("download not found")
	}

	if entry.Status == "completed" {
		return fmt.Errorf("download already completed")
	}

	settings, _ := config.LoadSettings()
	if settings == nil {
		settings = config.DefaultSettings()
	}

	// Reconstruct configuration
	outputPath := settings.General.DefaultDownloadDir
	if outputPath == "" {
		outputPath = "."
	}

	// Load saved state
	savedState, stateErr := state.LoadState(entry.URL, entry.DestPath)

	var mirrorURLs []string
	var dmState *types.ProgressState

	if stateErr == nil && savedState != nil {
		dmState = types.NewProgressState(id, savedState.TotalSize)
		dmState.Downloaded.Store(savedState.Downloaded)
		if savedState.Elapsed > 0 {
			dmState.SetSavedElapsed(time.Duration(savedState.Elapsed))
		}
		if len(savedState.Mirrors) > 0 {
			var mirrors []types.MirrorStatus
			for _, u := range savedState.Mirrors {
				mirrors = append(mirrors, types.MirrorStatus{URL: u, Active: true})
				mirrorURLs = append(mirrorURLs, u)
			}
			dmState.SetMirrors(mirrors)
		}
	} else {
		dmState = types.NewProgressState(id, entry.TotalSize)
		dmState.Downloaded.Store(entry.Downloaded)
		mirrorURLs = []string{entry.URL}
	}

	cfg := types.DownloadConfig{
		URL:        entry.URL,
		OutputPath: outputPath,
		DestPath:   entry.DestPath,
		ID:         id,
		Filename:   entry.Filename,
		Verbose:    false,
		IsResume:   true,
		ProgressCh: s.InputCh,
		State:      dmState,
		Runtime:    convertRuntimeConfig(settings.ToRuntimeConfig()),
		Mirrors:    mirrorURLs,
	}

	s.Pool.Add(cfg)
	return nil
}

// Delete cancels and removes a download.
func (s *LocalDownloadService) Delete(id string) error {
	if s.Pool == nil {
		return fmt.Errorf("worker pool not initialized")
	}

	s.Pool.Cancel(id)
	if err := state.RemoveFromMasterList(id); err != nil {
		return err
	}
	return nil
}

// convertRuntimeConfig helper (internal copy)
func convertRuntimeConfig(rc *config.RuntimeConfig) *types.RuntimeConfig {
	return &types.RuntimeConfig{
		MaxConnectionsPerHost: rc.MaxConnectionsPerHost,
		MaxGlobalConnections:  rc.MaxGlobalConnections,
		UserAgent:             rc.UserAgent,
		SequentialDownload:    rc.SequentialDownload,
		MinChunkSize:          rc.MinChunkSize,
		WorkerBufferSize:      rc.WorkerBufferSize,
		MaxTaskRetries:        rc.MaxTaskRetries,
		SlowWorkerThreshold:   rc.SlowWorkerThreshold,
		SlowWorkerGracePeriod: rc.SlowWorkerGracePeriod,
		StallTimeout:          rc.StallTimeout,
		SpeedEmaAlpha:         rc.SpeedEmaAlpha,
	}
}
