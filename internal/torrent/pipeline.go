package torrent

import (
	"sync"
)

const blockSize = 16 * 1024

type BlockPipeline struct {
	mu          sync.Mutex
	pieceIndex  int
	pieceSize   int64
	received    int64
	completed   bool
	inflight    map[int64]int64
	blockOffset int64
}

func NewBlockPipeline(pieceIndex int, pieceSize int64) *BlockPipeline {
	return &BlockPipeline{
		pieceIndex: pieceIndex,
		pieceSize:  pieceSize,
		inflight:   make(map[int64]int64),
	}
}

func (p *BlockPipeline) NextRequest() (begin int64, length int64, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.completed {
		return 0, 0, false
	}
	if p.blockOffset >= p.pieceSize {
		return 0, 0, false
	}
	begin = p.blockOffset
	length = blockSize
	if begin+length > p.pieceSize {
		length = p.pieceSize - begin
	}
	p.inflight[begin] = length
	p.blockOffset += length
	return begin, length, true
}

func (p *BlockPipeline) OnBlock(begin int64, length int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.inflight[begin]; ok {
		delete(p.inflight, begin)
		p.received += length
		if p.received >= p.pieceSize {
			p.completed = true
		}
	}
}

func (p *BlockPipeline) Completed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.completed
}
