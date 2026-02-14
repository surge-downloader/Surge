package torrent

import "sync"

type PiecePicker struct {
	totalPieces int
	mu          sync.Mutex
	queue       []int
	state       []uint8
}

func NewPiecePicker(totalPieces int) *PiecePicker {
	if totalPieces < 0 {
		totalPieces = 0
	}
	p := &PiecePicker{
		totalPieces: totalPieces,
		queue:       make([]int, 0, totalPieces),
		state:       make([]uint8, totalPieces),
	}
	for i := 0; i < totalPieces; i++ {
		p.queue = append(p.queue, i)
	}
	return p
}

func (p *PiecePicker) Next() (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for len(p.queue) > 0 {
		idx := p.queue[0]
		p.queue = p.queue[1:]
		if idx < 0 || idx >= p.totalPieces {
			continue
		}
		if p.state[idx] != 0 {
			continue
		}
		p.state[idx] = 1
		return idx, true
	}
	return 0, false
}

func (p *PiecePicker) Done(piece int) {
	if piece < 0 || piece >= p.totalPieces {
		return
	}
	p.mu.Lock()
	p.state[piece] = 2
	p.mu.Unlock()
}

func (p *PiecePicker) Requeue(piece int) {
	if piece < 0 || piece >= p.totalPieces {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.state[piece] != 1 {
		return
	}
	p.state[piece] = 0
	p.queue = append(p.queue, piece)
}
