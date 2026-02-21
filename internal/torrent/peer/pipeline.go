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
	if p.completed || p.inFlight >= p.maxInFlight {
		return 0, 0, false
	}
	for i, s := range p.states {
		if s == statePending {
			p.states[i] = stateInFlight
			p.inFlight++

			begin = int64(i) * defaultBlockSize
			length = defaultBlockSize
			if begin+length > p.pieceSize {
				length = p.pieceSize - begin
			}
			return begin, length, true
		}
	}
	return 0, 0, false
}

func (p *SimplePipeline) ResetInFlight() int {
	resetCount := 0
	for i, s := range p.states {
		if s == stateInFlight {
			p.states[i] = statePending
			resetCount++
		}
	}
	p.inFlight -= resetCount
	return resetCount
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
