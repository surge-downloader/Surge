package torrent

import "sync"

type PiecePicker struct {
	totalPieces int
	mu          sync.Mutex
	next        int
}

func NewPiecePicker(totalPieces int) *PiecePicker {
	if totalPieces < 0 {
		totalPieces = 0
	}
	return &PiecePicker{totalPieces: totalPieces}
}

func (p *PiecePicker) Next() (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.next >= p.totalPieces {
		return 0, false
	}
	idx := p.next
	p.next++
	return idx, true
}
