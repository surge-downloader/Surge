package peer

import (
	"context"
	"net"
	"sync"
	"time"
)

type Conn struct {
	sess     *Session
	addr     net.TCPAddr
	mu       sync.Mutex
	choked   bool
	bitfield []byte
	picker   Picker
	pl       PieceLayout
	store    Storage
	pipeline Pipeline
}

type Picker interface {
	Next() (int, bool)
}

type PieceLayout interface {
	PieceSize(pieceIndex int64) int64
}

type Storage interface {
	WriteAtPiece(pieceIndex int64, pieceOffset int64, data []byte) error
	VerifyPiece(pieceIndex int64) (bool, error)
}

func NewConn(sess *Session, addr net.TCPAddr, picker Picker, pl PieceLayout, store Storage, pipeline Pipeline) *Conn {
	return &Conn{
		sess:     sess,
		addr:     addr,
		choked:   true,
		picker:   picker,
		pl:       pl,
		store:    store,
		pipeline: pipeline,
	}
}

func (c *Conn) Start(ctx context.Context) {
	go c.readLoop(ctx)
	_ = WriteMessage(c.sess.conn, &Message{ID: MsgInterested})
}

func (c *Conn) readLoop(ctx context.Context) {
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
		c.handle(msg)
	}
}

func (c *Conn) handle(msg *Message) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch msg.ID {
	case MsgChoke:
		c.choked = true
	case MsgUnchoke:
		c.choked = false
		c.maybeRequest()
	case MsgBitfield:
		c.bitfield = append([]byte(nil), msg.Payload...)
	case MsgHave:
		// TODO: update bitfield (needs piece index parse)
	case MsgPiece:
		index, begin, block, err := ParsePiece(msg)
		if err == nil && c.store != nil && c.pipeline != nil {
			_ = c.store.WriteAtPiece(int64(index), int64(begin), block)
			c.pipeline.OnBlock(int64(begin), int64(len(block)))
			if c.pipeline.Completed() {
				_, _ = c.store.VerifyPiece(int64(index))
			}
		}
	default:
	}
}

func (c *Conn) maybeRequest() {
	if c.picker == nil || c.pl == nil {
		return
	}
	if c.choked {
		return
	}
	piece, ok := c.picker.Next()
	if !ok {
		return
	}
	if c.pipeline == nil {
		return
	}
	begin, length, ok := c.pipeline.NextRequest()
	if !ok {
		return
	}
	msg := MakeRequest(uint32(piece), uint32(begin), uint32(length))
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
