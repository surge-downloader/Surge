package torrent

import (
	"math/rand/v2"
	"sync"
)

type PiecePicker struct {
	totalPieces      int
	endgameThreshold int
	mu               sync.Mutex
	queue            []int
	state            []uint8
	availability     []uint16
}

func NewPiecePicker(totalPieces int) *PiecePicker {
	if totalPieces < 0 {
		totalPieces = 0
	}
	threshold := totalPieces / 20 // 5% of total
	if threshold > 20 {
		threshold = 20
	}
	if threshold < 2 {
		threshold = 2
	}
	p := &PiecePicker{
		totalPieces:      totalPieces,
		endgameThreshold: threshold,
		queue:            make([]int, 0, totalPieces),
		state:            make([]uint8, totalPieces),
		availability:     make([]uint16, totalPieces),
	}
	for i := 0; i < totalPieces; i++ {
		p.queue = append(p.queue, i)
	}

	// Shuffle queue so peers request random pieces instead of sequentially [0...N]
	// during equal availability priority weighting (which occurs aggressively on torrent boot).
	rand.Shuffle(len(p.queue), func(i, j int) {
		p.queue[i], p.queue[j] = p.queue[j], p.queue[i]
	})

	return p
}

func (p *PiecePicker) Next() (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.nextLocked(nil)
}

func (p *PiecePicker) NextFromBitfield(bitfield []byte) (int, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.nextLocked(bitfield)
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

func (p *PiecePicker) ObserveBitfield(bitfield []byte) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for byteIndex, b := range bitfield {
		if b == 0 {
			continue
		}
		base := byteIndex * 8
		for bit := 0; bit < 8; bit++ {
			if b&(1<<(7-bit)) == 0 {
				continue
			}
			piece := base + bit
			if piece >= p.totalPieces {
				break
			}
			p.incrementAvailabilityLocked(piece)
		}
	}
}

func (p *PiecePicker) ObserveHave(piece int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.incrementAvailabilityLocked(piece)
}

func (p *PiecePicker) incrementAvailabilityLocked(piece int) {
	if piece < 0 || piece >= p.totalPieces {
		return
	}
	if p.availability[piece] < ^uint16(0) {
		p.availability[piece]++
	}
}

// Remaining returns the number of pieces not yet completed (pending + in-flight).
func (p *PiecePicker) Remaining() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.remainingLocked()
}

func (p *PiecePicker) remainingLocked() int {
	done := 0
	for _, s := range p.state {
		if s == 2 {
			done++
		}
	}
	return p.totalPieces - done
}

// IsEndgame returns true when the unassigned piece count (pending)
// is below the endgame threshold. In endgame mode, multiple peers can request
// the same in-flight piece to avoid tail latency from slow peers.
func (p *PiecePicker) IsEndgame() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.queue) <= p.endgameThreshold
}

func (p *PiecePicker) NextFromBitfieldEndgame(bitfield []byte, skip []int) (int, bool, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// First try to pick a pending piece normally.
	if piece, ok := p.nextLocked(bitfield); ok {
		return piece, false, true
	}

	skipMap := make(map[int]bool)
	for _, s := range skip {
		skipMap[s] = true
	}

	var candidates []int
	bestAvailability := int(^uint(0) >> 1)

	for i := 0; i < p.totalPieces; i++ {
		if p.state[i] != 1 {
			continue
		}
		if skipMap[i] {
			continue
		}
		if len(bitfield) > 0 && !bitfieldHas(bitfield, i) {
			continue
		}
		avail := int(p.availability[i])
		if avail == 0 {
			avail = 1
		}
		if avail < bestAvailability {
			bestAvailability = avail
			candidates = candidates[:0]
			candidates = append(candidates, i)
		} else if avail == bestAvailability {
			candidates = append(candidates, i)
		}
	}
	if len(candidates) == 0 {
		return 0, false, false
	}

	idx := candidates[rand.IntN(len(candidates))]
	return idx, true, true
}

func (p *PiecePicker) nextLocked(peerBitfield []byte) (int, bool) {
	bestPos := -1
	bestAvailability := int(^uint(0) >> 1)

	validFound := 0

	for i, idx := range p.queue {
		if idx < 0 || idx >= p.totalPieces {
			continue
		}
		if p.state[idx] != 0 {
			continue
		}
		if len(peerBitfield) > 0 && !bitfieldHas(peerBitfield, idx) {
			continue
		}

		availability := int(p.availability[idx])
		if availability == 0 {
			availability = 1
		}
		if bestPos == -1 || availability < bestAvailability {
			bestPos = i
			bestAvailability = availability
			if availability == 1 {
				break
			}
		}
		validFound++
		// Performance: Approximate Rarest-First. Bound the scan to O(1)
		// instead of O(N) by selecting the best among the first 128 valid pieces.
		if validFound >= 128 {
			break
		}
	}

	if bestPos == -1 {
		return 0, false
	}

	idx := p.queue[bestPos]
	last := len(p.queue) - 1
	p.queue[bestPos] = p.queue[last]
	p.queue = p.queue[:last]
	p.state[idx] = 1
	return idx, true
}

func bitfieldHas(bitfield []byte, piece int) bool {
	if piece < 0 {
		return false
	}
	byteIndex := piece >> 3
	if byteIndex >= len(bitfield) {
		return false
	}
	mask := byte(1 << (7 - (piece & 7)))
	return bitfield[byteIndex]&mask != 0
}
