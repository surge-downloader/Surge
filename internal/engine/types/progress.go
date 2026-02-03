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

	// Chunk Visualization (Bitmap)
	ChunkBitmap     []byte // 2 bits per chunk
	ActualChunkSize int64  // Size of each actual chunk in bytes
	BitmapWidth     int    // Number of chunks tracked

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
	ChunkPending     ChunkStatus = 0 // 00
	ChunkDownloading ChunkStatus = 1 // 01
	ChunkCompleted   ChunkStatus = 2 // 10 (Bit 2 set)
)

// InitBitmap initializes the chunk bitmap
func (ps *ProgressState) InitBitmap(totalSize int64, chunkSize int64) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	// If already initialized with same parameters, skip
	if len(ps.ChunkBitmap) > 0 && ps.TotalSize == totalSize && ps.ActualChunkSize == chunkSize {
		return
	}

	if chunkSize <= 0 {
		return
	}

	numChunks := int((totalSize + chunkSize - 1) / chunkSize)

	// 2 bits per chunk. 4 chunks per byte.
	// Bytes needed = ceil(numChunks / 4)
	bytesNeeded := (numChunks + 3) / 4

	ps.ActualChunkSize = chunkSize
	ps.BitmapWidth = numChunks
	ps.ChunkBitmap = make([]byte, bytesNeeded)
}

// SetChunkState sets the 2-bit state for a specific chunk index
// State: 0=Pending, 1=Downloading, 2=Completed
func (ps *ProgressState) SetChunkState(index int, status ChunkStatus) {
	if index < 0 || index >= ps.BitmapWidth {
		return
	}

	byteIndex := index / 4
	bitOffset := (index % 4) * 2

	// Clear 2 bits at offset
	mask := byte(3 << bitOffset) // 00000011 shifted
	ps.ChunkBitmap[byteIndex] &= ^mask

	// Set new value
	val := byte(status) << bitOffset
	ps.ChunkBitmap[byteIndex] |= val
}

// GetChunkState gets the 2-bit state for a specific chunk index
func (ps *ProgressState) GetChunkState(index int) ChunkStatus {
	if index < 0 || index >= ps.BitmapWidth {
		return ChunkPending
	}

	byteIndex := index / 4
	bitOffset := (index % 4) * 2

	val := (ps.ChunkBitmap[byteIndex] >> bitOffset) & 3
	return ChunkStatus(val)
}

// UpdateChunkStatus updates the bitmap based on byte range
func (ps *ProgressState) UpdateChunkStatus(offset, length int64, status ChunkStatus) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.ActualChunkSize == 0 || len(ps.ChunkBitmap) == 0 {
		return
	}

	startIdx := int(offset / ps.ActualChunkSize)
	endIdx := int((offset + length - 1) / ps.ActualChunkSize)

	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx >= ps.BitmapWidth {
		endIdx = ps.BitmapWidth - 1
	}

	for i := startIdx; i <= endIdx; i++ {
		// Only upgrade status (Pending -> Downloading -> Completed)
		// Or if we are setting Downloading, don't overwrite Completed
		current := ps.GetChunkState(i)

		if status == ChunkCompleted {
			ps.SetChunkState(i, ChunkCompleted)
		} else if status == ChunkDownloading {
			if current != ChunkCompleted {
				ps.SetChunkState(i, ChunkDownloading)
			}
		}
	}
}

// GetBitmap returns a copy of the bitmap and metadata
func (ps *ProgressState) GetBitmap() ([]byte, int) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if len(ps.ChunkBitmap) == 0 {
		return nil, 0
	}

	result := make([]byte, len(ps.ChunkBitmap))
	copy(result, ps.ChunkBitmap)
	return result, ps.BitmapWidth
}
