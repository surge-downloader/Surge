package torrent

import (
	"sync"

	"github.com/surge-downloader/surge/internal/engine/types"
)

// ProgressStore wraps FileLayout to update progress state on verified pieces.
type ProgressStore struct {
	layout     *FileLayout
	state      *types.ProgressState
	mu         sync.Mutex
	verified   map[int64]bool
	bitfield   []byte
	onVerified func(int)
}

func NewProgressStore(layout *FileLayout, state *types.ProgressState) *ProgressStore {
	totalPieces := int((layout.TotalLength + layout.Info.PieceLength - 1) / layout.Info.PieceLength)
	var bitfield []byte
	if totalPieces > 0 {
		bitfield = make([]byte, (totalPieces+7)/8)
	}

	if state != nil {
		state.InitBitmap(layout.TotalLength, layout.Info.PieceLength)
	}

	store := &ProgressStore{
		layout:   layout,
		state:    state,
		verified: make(map[int64]bool),
		bitfield: bitfield,
	}

	// Fast resume: parse resumed bitmap to restore internal bitfield and visual chunk progress
	if state != nil {
		bitmap, _, _, chunkSize, _ := state.GetBitmap()
		if len(bitmap) > 0 && chunkSize > 0 {
			numChunks := int((layout.TotalLength + chunkSize - 1) / chunkSize)
			for i := 0; i < numChunks; i++ {
				// We reconstruct chunk progress from the saved 2-bit state if it's completed
				if state.GetChunkState(i) == types.ChunkCompleted {
					pieceSize := store.layout.PieceSize(int64(i))
					if pieceSize > 0 {
						// Mark piece verified locally in the store
						store.verified[int64(i)] = true
						if len(store.bitfield) > 0 {
							byteIndex := int(i >> 3)
							if byteIndex >= 0 && byteIndex < len(store.bitfield) {
								bit := uint(7 - (i & 7))
								store.bitfield[byteIndex] |= 1 << bit
							}
						}
						// Fast-forward chunk visualization without messing up global byte counters
						state.SetChunkProgressDirect(i, pieceSize)
					}
				}
			}
		}
	}

	return store
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
		s.state.UpdateChunkProgress(int(pieceIndex), inc)
		if s.state.GetChunkState(int(pieceIndex)) == types.ChunkPending {
			s.state.SetChunkState(int(pieceIndex), types.ChunkDownloading)
		}
		s.state.VerifiedProgress.Add(inc)
		s.state.Downloaded.Add(inc)
	}
	return nil
}

func (s *ProgressStore) ReadAtPiece(pieceIndex int64, pieceOffset int64, length int64) ([]byte, error) {
	return s.layout.ReadAtPiece(pieceIndex, pieceOffset, length)
}

func (s *ProgressStore) VerifyPiece(pieceIndex int64) (bool, error) {
	ok, err := s.layout.VerifyPiece(pieceIndex)
	if !ok || err != nil {
		return ok, err
	}
	return s.markPieceVerified(pieceIndex)
}

func (s *ProgressStore) VerifyPieceData(pieceIndex int64, data []byte) (bool, error) {
	ok, err := s.layout.VerifyPieceData(pieceIndex, data)
	if !ok || err != nil {
		return ok, err
	}
	return s.markPieceVerified(pieceIndex)
}

func (s *ProgressStore) markPieceVerified(pieceIndex int64) (bool, error) {
	pieceSize := s.layout.PieceSize(pieceIndex)
	if pieceSize <= 0 {
		return true, nil
	}

	var notify func(int)
	s.mu.Lock()
	if s.verified[pieceIndex] {
		s.mu.Unlock()
		return true, nil
	}
	s.verified[pieceIndex] = true
	if len(s.bitfield) > 0 {
		byteIndex := int(pieceIndex >> 3)
		if byteIndex >= 0 && byteIndex < len(s.bitfield) {
			bit := uint(7 - (pieceIndex & 7))
			s.bitfield[byteIndex] |= 1 << bit
		}
	}
	notify = s.onVerified
	s.mu.Unlock()

	if notify != nil {
		notify(int(pieceIndex))
	}
	if s.state != nil {
		s.state.SetChunkState(int(pieceIndex), types.ChunkCompleted)
		if pieceSize > 0 {
			s.state.UpdateChunkProgress(int(pieceIndex), pieceSize)
		}
	}
	return true, nil
}

func (s *ProgressStore) HasPiece(pieceIndex int64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.verified[pieceIndex]
}

func (s *ProgressStore) Bitfield() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.bitfield) == 0 {
		return nil
	}
	out := make([]byte, len(s.bitfield))
	copy(out, s.bitfield)
	return out
}

func (s *ProgressStore) SetOnVerified(fn func(int)) {
	s.mu.Lock()
	s.onVerified = fn
	s.mu.Unlock()
}
