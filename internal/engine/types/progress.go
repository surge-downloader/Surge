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
	mu      sync.Mutex     // Protects TotalSize, StartTime, SessionStartBytes, SavedElapsed, Mirrors
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
