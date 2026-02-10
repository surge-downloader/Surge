package types

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/surge-downloader/surge/internal/utils"
)

type ProgressState struct {
	ID            string
	Downloaded    atomic.Int64
	TotalSize     int64
	DestPath      string // Full absolute path
	StartTime     time.Time
	ActiveWorkers atomic.Int32
	Done          atomic.Bool
	Error         atomic.Pointer[error]
	Paused        atomic.Bool
	Pausing       atomic.Bool // Intermediate state: Pause requested but workers not yet exited
	CancelFunc    context.CancelFunc

	VerifiedProgress  atomic.Int64  // Verified bytes written to disk (for UI progress)
	SessionStartBytes int64         // SessionStartBytes tracks how many bytes were already downloaded when the current session started
	SavedElapsed      time.Duration // Time spent in previous sessions

	Mirrors []MirrorStatus // Status of each mirror

	// Chunk Visualization (Bitmap)
	// Chunk Visualization (Bitmap)
	ChunkBitmap     []byte  // 2 bits per chunk
	ChunkProgress   []int64 // Bytes downloaded per chunk (runtime only, not persisted)
	ActualChunkSize int64   // Size of each actual chunk in bytes
	BitmapWidth     int     // Number of chunks tracked

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
	downloaded = ps.VerifiedProgress.Load()
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

	utils.Debug("InitBitmap: Total=%d, ChunkSize=%d", totalSize, chunkSize)

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
	ps.ChunkProgress = make([]int64, numChunks)
}

// RestoreBitmap restores the chunk bitmap from saved state
func (ps *ProgressState) RestoreBitmap(bitmap []byte, actualChunkSize int64) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if len(bitmap) == 0 || actualChunkSize <= 0 {
		return
	}

	utils.Debug("RestoreBitmap: Len=%d, ChunkSize=%d", len(bitmap), actualChunkSize)

	ps.ChunkBitmap = bitmap
	ps.ActualChunkSize = actualChunkSize

	// Recalculate width
	numChunks := int((ps.TotalSize + ps.ActualChunkSize - 1) / ps.ActualChunkSize)
	ps.BitmapWidth = numChunks

	// Re-initialize progress tracking (will be filled by RecalculateProgress)
	if len(ps.ChunkProgress) != numChunks {
		ps.ChunkProgress = make([]int64, numChunks)
	}
}

// SetChunkProgress updates chunk progress array from external sources (e.g. remote events).
func (ps *ProgressState) SetChunkProgress(progress []int64) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if len(progress) == 0 {
		return
	}
	if len(ps.ChunkProgress) != len(progress) {
		ps.ChunkProgress = make([]int64, len(progress))
	}
	copy(ps.ChunkProgress, progress)
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
		utils.Debug("UpdateChunkStatus skipped: ActualChunkSize=%d, BitmapLen=%d", ps.ActualChunkSize, len(ps.ChunkBitmap))
		return
	}

	// Lazily init progress array if missing
	if len(ps.ChunkProgress) != ps.BitmapWidth {
		utils.Debug("UpdateChunkStatus: Initializing ChunkProgress array (width=%d)", ps.BitmapWidth)
		ps.ChunkProgress = make([]int64, ps.BitmapWidth)
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
		// Calculate precise overlap with this chunk
		chunkStart := int64(i) * ps.ActualChunkSize
		chunkEnd := chunkStart + ps.ActualChunkSize
		if chunkEnd > ps.TotalSize {
			chunkEnd = ps.TotalSize
		}

		updateStart := offset
		if updateStart < chunkStart {
			updateStart = chunkStart
		}

		updateEnd := offset + length
		if updateEnd > chunkEnd {
			updateEnd = chunkEnd
		}

		overlap := updateEnd - updateStart
		if overlap < 0 {
			overlap = 0
		}

		switch status {
		case ChunkCompleted:
			// Accumulate bytes
			// Only add providing we don't exceed chunk size
			increment := overlap
			remainingSpace := (chunkEnd - chunkStart) - ps.ChunkProgress[i]

			if increment > remainingSpace {
				increment = remainingSpace
			}

			if increment > 0 {
				ps.ChunkProgress[i] += increment
				ps.VerifiedProgress.Add(increment)
			}

			if ps.ChunkProgress[i] >= (chunkEnd - chunkStart) {
				ps.ChunkProgress[i] = chunkEnd - chunkStart // clamp
				ps.SetChunkState(i, ChunkCompleted)
				// utils.Debug("Chunk %d completed (size=%d)", i, ps.ChunkProgress[i])
			} else {
				// Partial progress -> Downloading
				if ps.GetChunkState(i) != ChunkCompleted {
					ps.SetChunkState(i, ChunkDownloading)
				}
			}
		case ChunkDownloading:
			current := ps.GetChunkState(i)
			if current != ChunkCompleted {
				ps.SetChunkState(i, ChunkDownloading)
			}
		}
	}
}

// RecalculateProgress reconstructs ChunkProgress from remaining tasks (for resume)
func (ps *ProgressState) RecalculateProgress(remainingTasks []Task) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if ps.ActualChunkSize == 0 || ps.BitmapWidth == 0 {
		return
	}

	// 1. Assume everything is downloaded initially
	ps.ChunkProgress = make([]int64, ps.BitmapWidth)
	var totalVerified int64
	for i := 0; i < ps.BitmapWidth; i++ {
		chunkStart := int64(i) * ps.ActualChunkSize
		chunkEnd := chunkStart + ps.ActualChunkSize
		if chunkEnd > ps.TotalSize {
			chunkEnd = ps.TotalSize
		}
		ps.ChunkProgress[i] = chunkEnd - chunkStart
		totalVerified += ps.ChunkProgress[i]
	}

	// 2. Subtract remaining tasks
	for _, task := range remainingTasks {
		offset := task.Offset
		length := task.Length

		startIdx := int(offset / ps.ActualChunkSize)
		endIdx := int((offset + length - 1) / ps.ActualChunkSize)

		if startIdx < 0 {
			startIdx = 0
		}
		if endIdx >= ps.BitmapWidth {
			endIdx = ps.BitmapWidth - 1
		}

		for i := startIdx; i <= endIdx; i++ {
			chunkStart := int64(i) * ps.ActualChunkSize
			chunkEnd := chunkStart + ps.ActualChunkSize
			if chunkEnd > ps.TotalSize {
				chunkEnd = ps.TotalSize
			}

			taskStart := offset
			if taskStart < chunkStart {
				taskStart = chunkStart
			}

			taskEnd := offset + length
			if taskEnd > chunkEnd {
				taskEnd = chunkEnd
			}

			overlap := taskEnd - taskStart
			if overlap > 0 {
				ps.ChunkProgress[i] -= overlap
				totalVerified -= overlap // Subtract unverified bytes
			}
		}
	}

	// Store the recalculated verified progress
	ps.VerifiedProgress.Store(totalVerified)

	// 3. Update Bitmap based on calculated progress
	for i := 0; i < ps.BitmapWidth; i++ {
		chunkStart := int64(i) * ps.ActualChunkSize
		chunkEnd := chunkStart + ps.ActualChunkSize
		if chunkEnd > ps.TotalSize {
			chunkEnd = ps.TotalSize
		}
		chunkSize := chunkEnd - chunkStart

		if ps.ChunkProgress[i] >= chunkSize {
			ps.ChunkProgress[i] = chunkSize // clamp
			ps.SetChunkState(i, ChunkCompleted)
		} else if ps.ChunkProgress[i] > 0 {
			// Even if saved bitmap said Pending, if we have bytes, it's actually partial
			ps.SetChunkState(i, ChunkDownloading)
		} else {
			ps.ChunkProgress[i] = 0 // clamp
			ps.SetChunkState(i, ChunkPending)
		}
	}
}

// GetBitmap returns a copy of the bitmap and metadata
func (ps *ProgressState) GetBitmap() ([]byte, int, int64, int64, []int64) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	if len(ps.ChunkBitmap) == 0 {
		return nil, 0, 0, 0, nil
	}

	result := make([]byte, len(ps.ChunkBitmap))
	copy(result, ps.ChunkBitmap)

	progressResult := make([]int64, len(ps.ChunkProgress))
	copy(progressResult, ps.ChunkProgress)

	return result, ps.BitmapWidth, ps.TotalSize, ps.ActualChunkSize, progressResult
}
