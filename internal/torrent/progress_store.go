package torrent

import (
	"sync"

	"github.com/surge-downloader/surge/internal/engine/types"
)

// ProgressStore wraps FileLayout to update progress state on verified pieces.
type ProgressStore struct {
	layout   *FileLayout
	state    *types.ProgressState
	mu       sync.Mutex
	verified map[int64]bool
}

func NewProgressStore(layout *FileLayout, state *types.ProgressState) *ProgressStore {
	return &ProgressStore{
		layout:   layout,
		state:    state,
		verified: make(map[int64]bool),
	}
}

func (s *ProgressStore) WriteAtPiece(pieceIndex int64, pieceOffset int64, data []byte) error {
	if err := s.layout.WriteAtPiece(pieceIndex, pieceOffset, data); err != nil {
		return err
	}
	if s.state == nil {
		return nil
	}
	inc := int64(len(data))
	if inc > 0 {
		s.state.VerifiedProgress.Add(inc)
		s.state.Downloaded.Add(inc)
	}
	return nil
}

func (s *ProgressStore) VerifyPiece(pieceIndex int64) (bool, error) {
	ok, err := s.layout.VerifyPiece(pieceIndex)
	if !ok || err != nil {
		return ok, err
	}
	if s.state == nil {
		return ok, nil
	}
	pieceSize := s.layout.PieceSize(pieceIndex)
	if pieceSize <= 0 {
		return ok, nil
	}

	s.mu.Lock()
	if s.verified[pieceIndex] {
		s.mu.Unlock()
		return ok, nil
	}
	s.verified[pieceIndex] = true
	s.mu.Unlock()

	return ok, nil
}
