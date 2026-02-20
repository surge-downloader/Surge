package peer

// Adapter to avoid circular dependency on torrent package.
type Pipeline interface {
	NextRequest() (begin int64, length int64, ok bool)
	OnBlock(begin int64, length int64)
	Completed() bool
	SetMaxInFlight(n int)
}

const defaultBlockSize = 16 * 1024

type blockState uint8

const (
	statePending  blockState = 0
	stateInFlight blockState = 1
	stateReceived blockState = 2
)

type SimplePipeline struct {
	pieceSize   int64
	numBlocks   int
	states      []blockState
	blockOffset int64
	received    int64
	inFlight    int
	maxInFlight int
	completed   bool
}

func NewSimplePipeline(pieceSize int64, maxInFlight int) *SimplePipeline {
	p := &SimplePipeline{
		maxInFlight: maxInFlight,
	}
	p.init(pieceSize, maxInFlight)
	return p
}

func (p *SimplePipeline) init(pieceSize int64, maxInFlight int) {
	if maxInFlight <= 0 {
		maxInFlight = 1
	}
	numBlocks := int((pieceSize + defaultBlockSize - 1) / defaultBlockSize)
	p.pieceSize = pieceSize
	p.numBlocks = numBlocks
	p.blockOffset = 0
	p.received = 0
	p.inFlight = 0
	p.completed = false
	p.maxInFlight = maxInFlight

	if cap(p.states) < numBlocks {
		p.states = make([]blockState, numBlocks)
	} else {
		p.states = p.states[:numBlocks]
		for i := range p.states {
			p.states[i] = statePending
		}
	}
}

func (p *SimplePipeline) NextRequest() (begin int64, length int64, ok bool) {
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

	blockIndex := int(begin / defaultBlockSize)
	if p.states[blockIndex] == statePending {
		p.states[blockIndex] = stateInFlight
		p.inFlight++
	}
	p.blockOffset += length
	return begin, length, true
}

func (p *SimplePipeline) OnBlock(begin int64, length int64) {
	blockIndex := int(begin / defaultBlockSize)
	if blockIndex < 0 || blockIndex >= p.numBlocks {
		return
	}

	if p.states[blockIndex] == stateReceived {
		return
	}
	if p.states[blockIndex] == stateInFlight {
		p.inFlight--
	}
	p.states[blockIndex] = stateReceived

	expected := int64(defaultBlockSize)
	if begin+expected > p.pieceSize {
		expected = p.pieceSize - begin
	}

	if length > expected {
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

func (p *SimplePipeline) Completed() bool {
	return p.completed
}

func (p *SimplePipeline) SetMaxInFlight(n int) {
	if n <= 0 {
		n = 1
	}
	p.maxInFlight = n
}
