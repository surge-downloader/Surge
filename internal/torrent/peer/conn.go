package peer

import (
	"context"
	"net"
	"sync"
	"time"
)

type Conn struct {
	sess      *Session
	addr      net.TCPAddr
	mu        sync.Mutex
	writeMu   sync.Mutex
	choked    bool
	amChoking bool
	bitfield  []byte
	picker    Picker
	pl        PieceLayout
	store     Storage
	pipeline  Pipeline
	piece     int
	onClose   func()
}

type Picker interface {
	Next() (int, bool)
}

type PieceLayout interface {
	PieceSize(pieceIndex int64) int64
}

type Storage interface {
	WriteAtPiece(pieceIndex int64, pieceOffset int64, data []byte) error
	ReadAtPiece(pieceIndex int64, pieceOffset int64, length int64) ([]byte, error)
	VerifyPiece(pieceIndex int64) (bool, error)
	HasPiece(pieceIndex int64) bool
	Bitfield() []byte
}

func NewConn(sess *Session, addr net.TCPAddr, picker Picker, pl PieceLayout, store Storage, pipeline Pipeline, onClose func()) *Conn {
	return &Conn{
		sess:      sess,
		addr:      addr,
		choked:    true,
		amChoking: true,
		picker:    picker,
		pl:        pl,
		store:     store,
		pipeline:  pipeline,
		piece:     -1,
		onClose:   onClose,
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
	case MsgHave:
		// TODO: update bitfield (needs piece index parse)
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
			_ = c.store.WriteAtPiece(int64(index), int64(begin), block)
			c.pipeline.OnBlock(int64(begin), int64(len(block)))
			if c.pipeline.Completed() {
				ok, _ := c.store.VerifyPiece(int64(index))
				if ok {
					c.advancePiece()
				} else if c.pl != nil {
					// Re-request the piece if verification fails
					c.pipeline = newSimplePipeline(c.pl.PieceSize(int64(index)))
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
	begin, length, ok := c.pipeline.NextRequest()
	if !ok {
		return
	}
	msg := MakeRequest(uint32(c.piece), uint32(begin), uint32(length))
	c.write(msg)
}

func (c *Conn) advancePiece() bool {
	if c.picker == nil || c.pl == nil {
		return false
	}
	piece, ok := c.picker.Next()
	if !ok {
		return false
	}
	size := c.pl.PieceSize(int64(piece))
	if size <= 0 {
		return false
	}
	c.piece = piece
	c.pipeline = newSimplePipeline(size)
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
