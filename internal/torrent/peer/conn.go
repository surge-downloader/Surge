package peer

import (
	"context"
	"net"
	"sync"
	"time"
)

type Conn struct {
	sess        *Session
	addr        net.TCPAddr
	mu          sync.Mutex
	writeMu     sync.Mutex
	choked      bool
	amChoking   bool
	bitfield    []byte
	picker      Picker
	pl          PieceLayout
	store       Storage
	pipeline    Pipeline
	maxInFlight int
	piece       int
	pieceBuf    []byte
	onClose     func()
	startedAt   time.Time
	lastPieceAt time.Time
	received    int64
	lastTuneAt  time.Time
	lastTuneRx  int64
}

type Perf struct {
	RateBps  float64
	Uptime   time.Duration
	IdleFor  time.Duration
	Received int64
	InFlight int
}

type Picker interface {
	Next() (int, bool)
	Done(piece int)
	Requeue(piece int)
}

type BitfieldAwarePicker interface {
	NextFromBitfield(bitfield []byte) (int, bool)
	ObserveBitfield(bitfield []byte)
	ObserveHave(piece int)
}

type PieceLayout interface {
	PieceSize(pieceIndex int64) int64
	VerifyPieceData(pieceIndex int64, data []byte) (bool, error)
}

type Storage interface {
	WriteAtPiece(pieceIndex int64, pieceOffset int64, data []byte) error
	ReadAtPiece(pieceIndex int64, pieceOffset int64, length int64) ([]byte, error)
	VerifyPiece(pieceIndex int64) (bool, error)
	VerifyPieceData(pieceIndex int64, data []byte) (bool, error)
	HasPiece(pieceIndex int64) bool
	Bitfield() []byte
}

func NewConn(sess *Session, addr net.TCPAddr, picker Picker, pl PieceLayout, store Storage, pipeline Pipeline, maxInFlight int, onClose func()) *Conn {
	if maxInFlight <= 0 {
		maxInFlight = 1
	}
	return &Conn{
		sess:        sess,
		addr:        addr,
		choked:      true,
		amChoking:   true,
		picker:      picker,
		pl:          pl,
		store:       store,
		pipeline:    pipeline,
		maxInFlight: maxInFlight,
		piece:       -1,
		onClose:     onClose,
		startedAt:   time.Now(),
	}
}

func (c *Conn) Start(ctx context.Context) {
	go c.readLoop(ctx)
	if c.store != nil {
		if bf := c.store.Bitfield(); len(bf) > 0 {
			c.write(&Message{ID: MsgBitfield, Payload: bf})
		}
	}
	c.write(&Message{ID: MsgInterested})
}

func (c *Conn) readLoop(ctx context.Context) {
	defer func() {
		c.mu.Lock()
		if c.picker != nil && c.piece >= 0 && c.pipeline != nil && !c.pipeline.Completed() {
			c.picker.Requeue(c.piece)
		}
		c.mu.Unlock()
		_ = c.sess.Close()
		if c.onClose != nil {
			c.onClose()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		_ = c.sess.conn.SetReadDeadline(time.Now().Add(10 * time.Second))
		msg, err := ReadMessage(c.sess.conn)
		if err != nil {
			return
		}
		shouldRequest, uploadReq := c.handle(msg)
		if uploadReq != nil {
			go c.sendPiece(*uploadReq)
		}
		if shouldRequest {
			c.maybeRequest()
		}
	}
}

type uploadRequest struct {
	index  uint32
	begin  uint32
	length uint32
}

func (c *Conn) handle(msg *Message) (bool, *uploadRequest) {
	requestNext := false
	var uploadReq *uploadRequest
	c.mu.Lock()
	defer c.mu.Unlock()
	switch msg.ID {
	case MsgChoke:
		c.choked = true
	case MsgUnchoke:
		c.choked = false
		requestNext = true
	case MsgBitfield:
		c.bitfield = append([]byte(nil), msg.Payload...)
		if bp, ok := c.picker.(BitfieldAwarePicker); ok {
			bp.ObserveBitfield(c.bitfield)
		}
		requestNext = true
	case MsgHave:
		idx, err := ParseHave(msg)
		if err != nil {
			break
		}
		if c.pl != nil && c.pl.PieceSize(int64(idx)) <= 0 {
			break
		}
		c.markHaveLocked(int(idx))
		if bp, ok := c.picker.(BitfieldAwarePicker); ok {
			bp.ObserveHave(int(idx))
		}
		requestNext = true
	case MsgRequest:
		index, begin, length, err := ParseRequest(msg)
		if err != nil {
			break
		}
		if c.amChoking || c.store == nil {
			break
		}
		if !c.store.HasPiece(int64(index)) {
			break
		}
		uploadReq = &uploadRequest{index: index, begin: begin, length: length}
	case MsgPiece:
		index, begin, block, err := ParsePiece(msg)
		if err == nil && c.store != nil && c.pipeline != nil {
			if !c.bufferPieceBlockLocked(int64(index), int64(begin), block) {
				break
			}
			c.observeBlockLocked(int64(len(block)))
			c.pipeline.OnBlock(int64(begin), int64(len(block)))
			if c.pipeline.Completed() {
				if len(c.pieceBuf) == 0 {
					c.resetCurrentPieceLocked()
					requestNext = true
					break
				}

				ok, err := c.pl.VerifyPieceData(int64(index), c.pieceBuf)
				if err != nil || !ok {
					c.resetCurrentPieceLocked()
					requestNext = true
					break
				}

				if err := c.store.WriteAtPiece(int64(index), 0, c.pieceBuf); err != nil {
					c.resetCurrentPieceLocked()
					requestNext = true
					break
				}

				ok, err = c.store.VerifyPieceData(int64(index), c.pieceBuf)
				if err == nil && ok {
					if c.picker != nil {
						c.picker.Done(int(index))
					}
					c.pieceBuf = nil
					c.advancePiece()
				} else if c.pl != nil {
					// Re-request the piece if verification fails
					c.resetCurrentPieceLocked()
				}
			}
			requestNext = true
		}
	default:
	}
	return requestNext, uploadReq
}

func (c *Conn) maybeRequest() {
	if c.picker == nil || c.pl == nil {
		return
	}
	if c.choked {
		return
	}
	if c.pipeline == nil {
		if !c.advancePiece() {
			return
		}
	}
	for {
		begin, length, ok := c.pipeline.NextRequest()
		if !ok {
			return
		}
		msg := MakeRequest(uint32(c.piece), uint32(begin), uint32(length))
		c.write(msg)
	}
}

func (c *Conn) advancePiece() bool {
	if c.picker == nil || c.pl == nil {
		return false
	}

	var (
		piece int
		ok    bool
	)

	if len(c.bitfield) > 0 {
		if bp, has := c.picker.(interface {
			NextFromBitfield(bitfield []byte) (int, bool)
		}); has {
			piece, ok = bp.NextFromBitfield(c.bitfield)
		} else {
			piece, ok = c.picker.Next()
		}
		if !ok {
			return false
		}
	} else {
		piece, ok = c.picker.Next()
		if !ok {
			return false
		}
	}
	size := c.pl.PieceSize(int64(piece))
	if size <= 0 {
		return false
	}
	c.piece = piece
	c.pipeline = newSimplePipeline(size, c.maxInFlight)
	if size > int64(maxInt) {
		c.pieceBuf = nil
		return false
	}
	c.pieceBuf = make([]byte, int(size))
	return true
}

func (c *Conn) SetChoke(choke bool) {
	c.mu.Lock()
	if c.amChoking == choke {
		c.mu.Unlock()
		return
	}
	c.amChoking = choke
	c.mu.Unlock()

	if choke {
		c.write(&Message{ID: MsgChoke})
		return
	}
	c.write(&Message{ID: MsgUnchoke})
}

func (c *Conn) SendHave(pieceIndex int) {
	if pieceIndex < 0 {
		return
	}
	c.write(MakeHave(uint32(pieceIndex)))
}

func (c *Conn) sendPiece(req uploadRequest) {
	c.mu.Lock()
	if c.amChoking || c.store == nil {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	data, err := c.store.ReadAtPiece(int64(req.index), int64(req.begin), int64(req.length))
	if err != nil || len(data) == 0 {
		return
	}
	c.write(MakePiece(req.index, req.begin, data))
}

func (c *Conn) write(msg *Message) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_ = WriteMessage(c.sess.conn, msg)
}

func (c *Conn) IsChoked() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.choked
}

func (c *Conn) Bitfield() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.bitfield == nil {
		return nil
	}
	out := make([]byte, len(c.bitfield))
	copy(out, c.bitfield)
	return out
}

func (c *Conn) observeBlockLocked(n int64) {
	if n <= 0 {
		return
	}
	now := time.Now()
	c.received += n
	c.lastPieceAt = now
	if c.lastTuneAt.IsZero() {
		c.lastTuneAt = now
		c.lastTuneRx = c.received
		return
	}

	elapsed := now.Sub(c.lastTuneAt)
	if elapsed < 2*time.Second {
		return
	}

	delta := c.received - c.lastTuneRx
	if delta < 0 {
		delta = 0
	}
	rate := float64(delta) / elapsed.Seconds()

	target := c.maxInFlight
	switch {
	case rate > 12*1024*1024:
		target += 2
	case rate > 4*1024*1024:
		target++
	case rate < 512*1024:
		target--
	}
	if target < 2 {
		target = 2
	}
	if target > 32 {
		target = 32
	}
	c.maxInFlight = target
	if c.pipeline != nil {
		c.pipeline.SetMaxInFlight(target)
	}

	c.lastTuneAt = now
	c.lastTuneRx = c.received
}

func (c *Conn) Performance() Perf {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	uptime := now.Sub(c.startedAt)
	if uptime <= 0 {
		uptime = time.Millisecond
	}
	idle := time.Duration(0)
	if !c.lastPieceAt.IsZero() {
		idle = now.Sub(c.lastPieceAt)
	}

	return Perf{
		RateBps:  float64(c.received) / uptime.Seconds(),
		Uptime:   uptime,
		IdleFor:  idle,
		Received: c.received,
		InFlight: c.maxInFlight,
	}
}

func (c *Conn) Close() {
	_ = c.sess.Close()
}

func (c *Conn) markHaveLocked(piece int) {
	if piece < 0 {
		return
	}
	byteIndex := piece / 8
	if byteIndex < 0 {
		return
	}
	if byteIndex >= len(c.bitfield) {
		expanded := make([]byte, byteIndex+1)
		copy(expanded, c.bitfield)
		c.bitfield = expanded
	}
	mask := byte(1 << (7 - (piece % 8)))
	c.bitfield[byteIndex] |= mask
}

func (c *Conn) bufferPieceBlockLocked(index int64, begin int64, block []byte) bool {
	if c.piece < 0 || int64(c.piece) != index {
		return false
	}
	if begin < 0 || len(block) == 0 {
		return false
	}
	if len(c.pieceBuf) == 0 {
		return false
	}
	end := begin + int64(len(block))
	if end > int64(len(c.pieceBuf)) {
		return false
	}
	copy(c.pieceBuf[int(begin):int(end)], block)
	return true
}

func (c *Conn) resetCurrentPieceLocked() {
	if c.pl == nil || c.piece < 0 {
		return
	}
	size := c.pl.PieceSize(int64(c.piece))
	if size <= 0 {
		return
	}
	c.pipeline = newSimplePipeline(size, c.maxInFlight)
	if size > int64(maxInt) {
		c.pieceBuf = nil
		return
	}
	c.pieceBuf = make([]byte, int(size))
}

const maxInt = int(^uint(0) >> 1)
