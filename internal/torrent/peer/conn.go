package peer

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

const (
	minAdaptiveInFlight = 16
	maxAdaptiveInFlight = 2048
	startupInFlightMin  = 256
	tuneWindow          = 1 * time.Second
)

type activePiece struct {
	index int
	sp    SimplePipeline
	buf   []byte
}

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
	active      []*activePiece
	inFlight    int
	maxInFlight int
	utPexID     byte
	onPEXPeer   func(net.TCPAddr)
	onClose     func(error)
	startedAt   time.Time
	lastPieceAt time.Time
	received    int64
	lastTuneAt  time.Time
	lastTuneRx  int64
	readBuf     []byte
	writeBuf    []byte
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

func NewConn(sess *Session, addr net.TCPAddr, picker Picker, pl PieceLayout, store Storage, maxInFlight int, onPEXPeer func(net.TCPAddr), onClose func(error)) *Conn {
	if maxInFlight <= 0 {
		maxInFlight = minAdaptiveInFlight
	}
	if maxInFlight < startupInFlightMin {
		maxInFlight = startupInFlightMin
	}
	if maxInFlight > maxAdaptiveInFlight {
		maxInFlight = maxAdaptiveInFlight
	}
	return &Conn{
		sess:        sess,
		addr:        addr,
		choked:      true,
		amChoking:   true,
		picker:      picker,
		pl:          pl,
		store:       store,
		maxInFlight: maxInFlight,
		onPEXPeer:   onPEXPeer,
		onClose:     onClose,
		startedAt:   time.Now(),
		readBuf:     make([]byte, 65536), // 64KB handles framing + 16KB typical MsgPiece
		writeBuf:    make([]byte, 65536),
	}
}

func (c *Conn) Start(ctx context.Context) {
	go c.readLoop(ctx)
	go c.keepAliveLoop(ctx)
	if c.sess != nil && c.sess.SupportsExtensionProtocol() {
		c.sendExtendedHandshake()
	}
	if c.store != nil {
		if bf := c.store.Bitfield(); len(bf) > 0 {
			c.write(&Message{ID: MsgBitfield, Payload: bf})
		}
	}
	c.write(&Message{ID: MsgInterested})
}

func (c *Conn) keepAliveLoop(ctx context.Context) {
	interval := peerKeepAliveSend
	if c.sess != nil {
		interval = c.sess.KeepAliveSendInterval()
	}
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.write(&Message{ID: 255})
		}
	}
}

func (c *Conn) readLoop(ctx context.Context) {
	var closeErr error
	defer func() {
		c.mu.Lock()
		if c.picker != nil {
			for _, ap := range c.active {
				if !ap.sp.Completed() {
					c.picker.Requeue(ap.index)
				}
			}
		}
		c.active = nil
		c.mu.Unlock()
		_ = c.sess.Close()
		if c.onClose != nil {
			c.onClose(closeErr)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		readTimeout := peerReadTimeout
		if c.sess != nil {
			readTimeout = c.sess.ReadTimeout()
		}
		_ = c.sess.conn.SetReadDeadline(time.Now().Add(readTimeout))
		msg, err := ReadMessageBuffered(c.sess.conn, &c.readBuf)
		if err != nil {
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				continue
			}
			closeErr = err
			return
		}
		shouldRequest, uploadReq, pexPeers := c.handle(msg)
		if uploadReq != nil {
			go c.sendPiece(*uploadReq)
		}
		if len(pexPeers) > 0 && c.onPEXPeer != nil {
			for _, addr := range pexPeers {
				c.onPEXPeer(addr)
			}
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

func (c *Conn) handle(msg *Message) (bool, *uploadRequest, []net.TCPAddr) {
	requestNext := false
	var uploadReq *uploadRequest
	var pexPeers []net.TCPAddr
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
		if err == nil && c.store != nil {
			var ap *activePiece
			for _, p := range c.active {
				if p.index == int(index) {
					ap = p
					break
				}
			}
			if ap == nil {
				break
			}
			if !c.bufferPieceBlockLocked(ap, int64(begin), block) {
				break
			}
			c.observeBlockLocked(int64(len(block)))
			ap.sp.OnBlock(int64(begin), int64(len(block)))
			c.inFlight--
			if c.inFlight < 0 {
				c.inFlight = 0
			}

			if ap.sp.Completed() {
				if len(ap.buf) == 0 {
					if c.picker != nil {
						c.picker.Requeue(ap.index)
					}
					c.removeActivePieceLocked(ap.index)
					requestNext = true
					break
				}

				buf := ap.buf
				ap.buf = nil // prevent reuse
				idx := ap.index
				c.removeActivePieceLocked(idx)

				requestNext = true

				go func(idx int, b []byte) {
					ok, err := c.pl.VerifyPieceData(int64(idx), b)
					if err != nil || !ok {
						if c.picker != nil {
							c.picker.Requeue(idx)
						}
						return
					}
					if err := c.store.WriteAtPiece(int64(idx), 0, b); err != nil {
						if c.picker != nil {
							c.picker.Requeue(idx)
						}
						return
					}
					ok, err = c.store.VerifyPieceData(int64(idx), b)
					if err == nil && ok {
						if c.picker != nil {
							c.picker.Done(idx)
						}
					} else {
						if c.picker != nil {
							c.picker.Requeue(idx)
						}
					}
				}(idx, buf)
			}
			requestNext = true
		}
	case MsgExtended:
		pexPeers = c.handleExtendedLocked(msg)
	default:
	}
	return requestNext, uploadReq, pexPeers
}

func (c *Conn) maybeRequest() {
	if c.picker == nil || c.pl == nil {
		return
	}
	if c.choked {
		return
	}

	for c.inFlight < c.maxInFlight {
		didRequest := false
		for _, ap := range c.active {
			begin, length, ok := ap.sp.NextRequest()
			if ok {
				msg := MakeRequest(uint32(ap.index), uint32(begin), uint32(length))
				c.write(msg)
				c.inFlight++
				didRequest = true
				if c.inFlight >= c.maxInFlight {
					break
				}
			}
		}
		if c.inFlight >= c.maxInFlight {
			break
		}
		if !didRequest {
			if !c.advancePiece() {
				break
			}
		}
	}
}

func (c *Conn) advancePiece() bool {
	if c.picker == nil || c.pl == nil {
		return false
	}

	maxActive := 32
	if c.maxInFlight > 0 {
		pieceSize := c.pl.PieceSize(0)
		if pieceSize > 0 {
			needed := (c.maxInFlight * 16384) / int(pieceSize)
			if needed+2 > maxActive {
				maxActive = needed + 2
			}
		}
	}
	if maxActive > 128 {
		maxActive = 128
	}
	if len(c.active) >= maxActive {
		return false
	}

	var (
		piece int
		ok    bool
	)

	if len(c.bitfield) > 0 {
		if bp, has := c.picker.(BitfieldAwarePicker); has {
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
	ap := &activePiece{index: piece}
	ap.sp.init(size, c.maxInFlight)
	ap.sp.SetMaxInFlight(maxInt)
	if size <= int64(maxInt) {
		ap.buf = make([]byte, int(size))
	}
	c.active = append(c.active, ap)
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

func (c *Conn) SendMessage(msg *Message) {
	if msg == nil {
		return
	}
	c.write(msg)
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

func (c *Conn) sendExtendedHandshake() {
	msg, err := MakeExtendedHandshakeMessage(map[string]byte{
		utPexExtensionName: utPexLocalMessageID,
	})
	if err != nil {
		return
	}
	c.write(msg)
}

func (c *Conn) handleExtendedLocked(msg *Message) []net.TCPAddr {
	extID, payload, err := ParseExtendedMessage(msg)
	if err != nil {
		return nil
	}
	if extID == extendedHandshakeID {
		hs, err := ParseExtendedHandshake(payload)
		if err != nil {
			return nil
		}
		if id, ok := hs.Messages[utPexExtensionName]; ok && id != 0 {
			c.utPexID = id
		}
		return nil
	}
	if c.utPexID == 0 || extID != c.utPexID {
		return nil
	}
	peers, err := ParseUTPexPeers(payload)
	if err != nil {
		return nil
	}
	return peers
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
	if elapsed < tuneWindow {
		return
	}

	delta := c.received - c.lastTuneRx
	if delta < 0 {
		delta = 0
	}
	rate := float64(delta) / elapsed.Seconds()

	target := c.maxInFlight
	switch {
	case rate > 32*1024*1024:
		target += 128
	case rate > 16*1024*1024:
		target += 64
	case rate > 8*1024*1024:
		target += 32
	case rate > 4*1024*1024:
		target += 16
	case rate > 1024*1024:
		target += 8
	case rate < 256*1024:
		target -= 8
	case rate < 1024*1024:
		target -= 2
	}
	if target < minAdaptiveInFlight {
		target = minAdaptiveInFlight
	}
	if target > maxAdaptiveInFlight {
		target = maxAdaptiveInFlight
	}
	c.maxInFlight = target

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

func (c *Conn) bufferPieceBlockLocked(ap *activePiece, begin int64, block []byte) bool {
	if begin < 0 || len(block) == 0 {
		return false
	}
	if len(ap.buf) == 0 {
		return false
	}
	end := begin + int64(len(block))
	if end > int64(len(ap.buf)) {
		return false
	}
	copy(ap.buf[int(begin):int(end)], block)
	return true
}

func (c *Conn) removeActivePieceLocked(idx int) {
	for i, ap := range c.active {
		if ap.index == idx {
			c.active[i] = c.active[len(c.active)-1]
			c.active[len(c.active)-1] = nil
			c.active = c.active[:len(c.active)-1]
			return
		}
	}
}

const maxInt = int(^uint(0) >> 1)
