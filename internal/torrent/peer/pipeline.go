package peer

// Adapter to avoid circular dependency on torrent package.
type Pipeline interface {
	NextRequest() (begin int64, length int64, ok bool)
	OnBlock(begin int64, length int64)
	Completed() bool
}

const defaultBlockSize = 16 * 1024

type simplePipeline struct {
	pieceSize   int64
	blockOffset int64
	received    int64
	completed   bool
}

func newSimplePipeline(pieceSize int64) *simplePipeline {
	return &simplePipeline{pieceSize: pieceSize}
}

func (p *simplePipeline) NextRequest() (begin int64, length int64, ok bool) {
	if p.completed {
		return 0, 0, false
	}
	if p.blockOffset >= p.pieceSize {
		return 0, 0, false
	}
	begin = p.blockOffset
	length = defaultBlockSize
	if begin+length > p.pieceSize {
		length = p.pieceSize - begin
	}
	p.blockOffset += length
	return begin, length, true
}

func (p *simplePipeline) OnBlock(begin int64, length int64) {
	p.received += length
	if p.received >= p.pieceSize {
		p.completed = true
	}
}

func (p *simplePipeline) Completed() bool {
	return p.completed
}
