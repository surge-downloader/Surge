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
}

func NewConn(sess *Session, addr net.TCPAddr) *Conn {
	return &Conn{
		sess:   sess,
		addr:   addr,
		choked: true,
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
	case MsgBitfield:
		c.bitfield = append([]byte(nil), msg.Payload...)
	case MsgHave:
		// TODO: update bitfield (needs piece index parse)
	default:
	}
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
