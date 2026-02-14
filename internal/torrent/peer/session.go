package peer

import (
	"context"
	"fmt"
	"net"
	"time"
)

type Session struct {
	conn net.Conn
}

func NewFromConn(conn net.Conn) *Session {
	return &Session{conn: conn}
}

func Dial(ctx context.Context, addr net.TCPAddr, infoHash [20]byte, peerID [20]byte) (*Session, error) {
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr.String())
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(8 * time.Second))
	if err := WriteHandshake(conn, Handshake{InfoHash: infoHash, PeerID: peerID}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	hs, err := ReadHandshake(conn)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if hs.InfoHash != infoHash {
		_ = conn.Close()
		return nil, fmt.Errorf("infohash mismatch")
	}
	_ = conn.SetDeadline(time.Time{})
	return &Session{conn: conn}, nil
}

func (s *Session) Close() error {
	if s.conn == nil {
		return nil
	}
	return s.conn.Close()
}
