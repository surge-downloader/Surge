package core

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/surge-downloader/surge/internal/config"
	"github.com/surge-downloader/surge/internal/download"
	"github.com/surge-downloader/surge/internal/engine/events"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/utils"
)

// ReloadSettings reloads settings from disk
func (s *LocalDownloadService) ReloadSettings() error {
	settings, err := config.LoadSettings()
	if err != nil {
		return err
	}
	s.settingsMu.Lock()
	s.settings = settings
	s.settingsMu.Unlock()
	return nil
}

// LocalDownloadService implements DownloadService for the local embedded engine.
type LocalDownloadService struct {
	Pool    *download.WorkerPool
	InputCh chan interface{}

	// Broadcast fields
	listeners  []chan interface{}
	listenerMu sync.Mutex

	reportTicker *time.Ticker

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc

	// Settings Cache
	settings   *config.Settings
	settingsMu sync.RWMutex
}

const (
	SpeedSmoothingAlpha = 0.3
	ReportInterval      = 150 * time.Millisecond
)

// NewLocalDownloadService creates a new specific service instance.
func NewLocalDownloadService(pool *download.WorkerPool) *LocalDownloadService {
	inputCh := make(chan interface{}, 100)
	s := &LocalDownloadService{
		Pool:      pool,
		InputCh:   inputCh,
		listeners: make([]chan interface{}, 0),
	}

	// Load initial settings
	if s.settings, _ = config.LoadSettings(); s.settings == nil {
		s.settings = config.DefaultSettings()
	}

	// Lifecycle
	ctx, cancel := context.WithCancel(context.Background())
	s.ctx = ctx
	s.cancel = cancel

	// Start broadcaster
	go s.broadcastLoop()

	// Start progress reporter
	if pool != nil {
		s.reportTicker = time.NewTicker(ReportInterval)
		go s.reportProgressLoop()
	}

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

	if s.reportTicker != nil {
		s.reportTicker.Stop()
	}
}

func (s *LocalDownloadService) reportProgressLoop() {
	lastSpeeds := make(map[string]float64)

	for range s.reportTicker.C {
		if s.Pool == nil {
			continue
		}

		activeConfigs := s.Pool.GetAll()
		for _, cfg := range activeConfigs {
			if cfg.State == nil || cfg.State.IsPaused() || cfg.State.Done.Load() {
				// Clean up speed history for inactive
				delete(lastSpeeds, cfg.ID)
				continue
			}

			// Calculate Progress
			downloaded, total, totalElapsed, sessionElapsed, connections, sessionStart := cfg.State.GetProgress()

			// Calculate Speed with EMA
			sessionDownloaded := downloaded - sessionStart
			var instantSpeed float64
			if sessionElapsed.Seconds() > 0 && sessionDownloaded > 0 {
				instantSpeed = float64(sessionDownloaded) / sessionElapsed.Seconds()
			}

			lastSpeed := lastSpeeds[cfg.ID]
			var currentSpeed float64
			if lastSpeed == 0 {
				currentSpeed = instantSpeed
			} else {
				currentSpeed = SpeedSmoothingAlpha*instantSpeed + (1-SpeedSmoothingAlpha)*lastSpeed
			}
			lastSpeeds[cfg.ID] = currentSpeed

			// Create Message
			msg := events.ProgressMsg{
				DownloadID:        cfg.ID,
				Downloaded:        downloaded,
				Total:             total,
				Speed:             currentSpeed,
				Elapsed:           totalElapsed,
				ActiveConnections: int(connections),
			}

			// Add Chunk Bitmap for visualization (if initialized)
			bitmap, width, _, _, _ := cfg.State.GetBitmap()
			if width > 0 && len(bitmap) > 0 {
				msg.ChunkBitmap = bitmap
				msg.BitmapWidth = width
			}

			// Send to InputCh (non-blocking)
			select {
			case s.InputCh <- msg:
			default:
			}
		}
	}
}

// StreamEvents returns a channel that receives real-time download events.
func (s *LocalDownloadService) StreamEvents() (<-chan interface{}, error) {
	ch := make(chan interface{}, 100)
	s.listenerMu.Lock()
	s.listeners = append(s.listeners, ch)
	s.listenerMu.Unlock()

	// Cleanup listener on context cancellation
	go func() {
		<-s.ctx.Done()
		s.listenerMu.Lock()
		defer s.listenerMu.Unlock()
		for i, listener := range s.listeners {
			if listener == ch {
				s.listeners = append(s.listeners[:i], s.listeners[i+1:]...)
				close(ch)
				break
			}
		}
	}()

	return ch, nil
}

// Shutdown stops the service.
func (s *LocalDownloadService) Shutdown() error {
	if s.reportTicker != nil {
		s.reportTicker.Stop()
	}
	if s.Pool != nil {
		s.Pool.GracefulShutdown()
	}

	// Stop listeners and broadcaster
	s.cancel()

	// Close input channel to stop broadcaster
	close(s.InputCh)
	return nil
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
				if cfg.State.DestPath != "" {
					status.DestPath = cfg.State.DestPath
				}

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
				DestPath:    d.DestPath,
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

	s.settingsMu.RLock()
	settings := s.settings
	s.settingsMu.RUnlock()

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
	state := types.NewProgressState(id, 0)
	state.DestPath = filepath.Join(outPath, filename) // Best guess until download starts

	cfg := types.DownloadConfig{
		URL:        url,
		Mirrors:    mirrors,
		OutputPath: outPath,
		ID:         id,
		Filename:   filename, // If empty, will be auto-detected
		Verbose:    false,
		ProgressCh: s.InputCh,
		State:      state,
		Runtime:    types.ConvertRuntimeConfig(settings.ToRuntimeConfig()),
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

	if entry.Status == "completed" {
		return fmt.Errorf("download already completed")
	}

	s.settingsMu.RLock()
	settings := s.settings
	s.settingsMu.RUnlock()

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
		dmState.DestPath = entry.DestPath
	} else {
		dmState = types.NewProgressState(id, entry.TotalSize)
		dmState.Downloaded.Store(entry.Downloaded)
		dmState.DestPath = entry.DestPath
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
		Runtime:    types.ConvertRuntimeConfig(settings.ToRuntimeConfig()),
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

// History returns completed downloads
func (s *LocalDownloadService) History() ([]types.DownloadEntry, error) {
	// For local service, we can directly access the state DB
	return state.LoadCompletedDownloads()
}
