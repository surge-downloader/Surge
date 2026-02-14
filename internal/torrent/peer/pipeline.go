package peer

// Adapter to avoid circular dependency on torrent package.
type Pipeline interface {
	NextRequest() (begin int64, length int64, ok bool)
	OnBlock(begin int64, length int64)
	Completed() bool
	SetMaxInFlight(n int)
}

const defaultBlockSize = 16 * 1024

type simplePipeline struct {
	pieceSize   int64
	blockOffset int64
	received    int64
	inFlight    int
	maxInFlight int
	completed   bool
}

func newSimplePipeline(pieceSize int64, maxInFlight int) *simplePipeline {
	if maxInFlight <= 0 {
		maxInFlight = 1
	}
	return &simplePipeline{
		pieceSize:   pieceSize,
		maxInFlight: maxInFlight,
	}
}

func (p *simplePipeline) NextRequest() (begin int64, length int64, ok bool) {
	if p.completed {
		return 0, 0, false
	}
	if p.inFlight >= p.maxInFlight {
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
	p.inFlight++
	return begin, length, true
}

func (p *simplePipeline) OnBlock(begin int64, length int64) {
	if p.inFlight > 0 {
		p.inFlight--
	}
	p.received += length
	if p.received >= p.pieceSize {
		p.completed = true
	}
}

func (p *simplePipeline) Completed() bool {
	return p.completed
}

func (p *simplePipeline) SetMaxInFlight(n int) {
	if n <= 0 {
		n = 1
	}
	p.maxInFlight = n
}
