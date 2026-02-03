package types

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

type ProgressState struct {
	ID            string
	Downloaded    atomic.Int64
	TotalSize     int64
	StartTime     time.Time
	ActiveWorkers atomic.Int32
	Done          atomic.Bool
	Error         atomic.Pointer[error]
	Paused        atomic.Bool
	Pausing       atomic.Bool // Intermediate state: Pause requested but workers not yet exited
	CancelFunc    context.CancelFunc

	SessionStartBytes int64         // SessionStartBytes tracks how many bytes were already downloaded when the current session started
	SavedElapsed      time.Duration // Time spent in previous sessions

	Mirrors []MirrorStatus // Status of each mirror

	// Chunk Visualization
	Chunks          []ChunkStatus // Grid of chunks for TUI
	ChunkProgress   []int64       // Bytes downloaded per chunk
	VisualChunkSize int64         // Size of each visualization chunk

	mu sync.Mutex // Protects TotalSize, StartTime, SessionStartBytes, SavedElapsed, Mirrors
}

type MirrorStatus struct {
	URL    string
	Active bool
	Error  bool
}

func NewProgressState(id string, totalSize int64) *ProgressState {
	return &ProgressState{
		ID:        id,
		TotalSize: totalSize,
		StartTime: time.Now(),
	}
}

func (ps *ProgressState) SetTotalSize(size int64) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.TotalSize = size
	ps.SessionStartBytes = ps.Downloaded.Load()
	ps.StartTime = time.Now()
}

func (ps *ProgressState) SyncSessionStart() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.SessionStartBytes = ps.Downloaded.Load()
	ps.StartTime = time.Now()
}

func (ps *ProgressState) SetError(err error) {
	ps.Error.Store(&err)
}

func (ps *ProgressState) GetError() error {
	if e := ps.Error.Load(); e != nil {
		return *e
	}
	return nil
}

func (ps *ProgressState) GetProgress() (downloaded int64, total int64, totalElapsed time.Duration, sessionElapsed time.Duration, connections int32, sessionStartBytes int64) {
	downloaded = ps.Downloaded.Load()
	connections = ps.ActiveWorkers.Load()

	ps.mu.Lock()
	total = ps.TotalSize
	sessionElapsed = time.Since(ps.StartTime)
	totalElapsed = ps.SavedElapsed + sessionElapsed
	sessionStartBytes = ps.SessionStartBytes
	ps.mu.Unlock()
	return
}

func (ps *ProgressState) Pause() {
	ps.Paused.Store(true)
	if ps.CancelFunc != nil {
		ps.CancelFunc()
	}
}

func (ps *ProgressState) Resume() {
	ps.Paused.Store(false)
}

func (ps *ProgressState) IsPaused() bool {
	return ps.Paused.Load()
}

func (ps *ProgressState) SetPausing(pausing bool) {
	ps.Pausing.Store(pausing)
}

func (ps *ProgressState) IsPausing() bool {
	return ps.Pausing.Load()
}

func (ps *ProgressState) SetSavedElapsed(d time.Duration) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.SavedElapsed = d
}

func (ps *ProgressState) SetMirrors(mirrors []MirrorStatus) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	// Deep copy to prevent race conditions if caller modifies the slice
	ps.Mirrors = make([]MirrorStatus, len(mirrors))
	copy(ps.Mirrors, mirrors)
}

func (ps *ProgressState) GetMirrors() []MirrorStatus {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	// Return a copy
	if len(ps.Mirrors) == 0 {
		return nil
	}
	mirrors := make([]MirrorStatus, len(ps.Mirrors))
	copy(mirrors, ps.Mirrors)
	return mirrors
}

// ChunkStatus represents the status of a visualization chunk
type ChunkStatus int

const (
	ChunkPending     ChunkStatus = iota
	ChunkDownloading             // Active
	ChunkCompleted
)

// InitChunks initializes the chunk map for visualization
// Target is around 200 chunks for the TUI grid
func (ps *ProgressState) InitChunks(totalSize int64) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// If already initialized and size matches, don't reset (resume case handled elsewhere)
	if len(ps.Chunks) > 0 && ps.TotalSize == totalSize {
		return
	}

	targetChunks := 200
	chunkSize := totalSize / int64(targetChunks)
	if chunkSize < 1024*1024 { // Minimum 1MB per block to avoid too much noise for small files
		chunkSize = 1024 * 1024
	}

	numChunks := int(totalSize / chunkSize)
	if totalSize%chunkSize != 0 {
		numChunks++
	}

	ps.VisualChunkSize = chunkSize
	ps.Chunks = make([]ChunkStatus, numChunks)
	ps.ChunkProgress = make([]int64, numChunks)
}

// UpdateChunkStatus updates the status of chunks covering the given range
func (ps *ProgressState) UpdateChunkStatus(offset, length int64, status ChunkStatus) {
	// Fast path check without lock
	if ps.VisualChunkSize == 0 {
		return
	}

	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.VisualChunkSize == 0 || len(ps.Chunks) == 0 {
		return
	}

	startIdx := int(offset / ps.VisualChunkSize)
	endIdx := int((offset + length - 1) / ps.VisualChunkSize)

	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx >= len(ps.Chunks) {
		endIdx = len(ps.Chunks) - 1
	}

	for i := startIdx; i <= endIdx; i++ {
		// Calculate chunk boundaries
		chunkStart := int64(i) * ps.VisualChunkSize
		chunkEnd := chunkStart + ps.VisualChunkSize
		if chunkEnd > ps.TotalSize {
			chunkEnd = ps.TotalSize
		}

		// Calculate overlap
		rangeStart := offset
		if rangeStart < chunkStart {
			rangeStart = chunkStart
		}
		rangeEnd := offset + length
		if rangeEnd > chunkEnd {
			rangeEnd = chunkEnd
		}

		overlap := rangeEnd - rangeStart
		if overlap < 0 {
			overlap = 0
		}

		// Logic for ChunkCompleted (accumulate bytes)
		if status == ChunkCompleted {
			ps.ChunkProgress[i] += overlap
			if ps.ChunkProgress[i] >= (chunkEnd - chunkStart) {
				ps.Chunks[i] = ChunkCompleted
			} else {
				// Partial progress logic:
				// If we have some progress, make sure it shows as downloading
				if ps.Chunks[i] == ChunkPending {
					ps.Chunks[i] = ChunkDownloading
				}
			}
		} else if status == ChunkDownloading {
			// For 'Downloading' status updates (starts of tasks),
			// just ensure it lights up as downloading if not already done
			if ps.Chunks[i] != ChunkCompleted {
				ps.Chunks[i] = ChunkDownloading
			}
		}
	}
}

// GetChunks returns a copy of the current chunk statuses
func (ps *ProgressState) GetChunks() []ChunkStatus {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if len(ps.Chunks) == 0 {
		return nil
	}

	result := make([]ChunkStatus, len(ps.Chunks))
	copy(result, ps.Chunks)
	return result
}
