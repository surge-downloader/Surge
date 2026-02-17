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
	inFlight    map[int64]int64
	receivedAt  map[int64]struct{}
	maxInFlight int
	completed   bool
}

func newSimplePipeline(pieceSize int64, maxInFlight int) *simplePipeline {
	if maxInFlight <= 0 {
		maxInFlight = 1
	}
	return &simplePipeline{
		pieceSize:   pieceSize,
		inFlight:    make(map[int64]int64),
		receivedAt:  make(map[int64]struct{}),
		maxInFlight: maxInFlight,
	}
}

func (p *simplePipeline) NextRequest() (begin int64, length int64, ok bool) {
	if p.completed {
		return 0, 0, false
	}
	if len(p.inFlight) >= p.maxInFlight {
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
	p.inFlight[begin] = length
	return begin, length, true
}

func (p *simplePipeline) OnBlock(begin int64, length int64) {
	expected, ok := p.inFlight[begin]
	if !ok {
		return
	}
	delete(p.inFlight, begin)

	if _, seen := p.receivedAt[begin]; seen {
		return
	}
	p.receivedAt[begin] = struct{}{}

	if expected > 0 && length > expected {
		length = expected
	}
	if length < 0 {
		length = 0
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
