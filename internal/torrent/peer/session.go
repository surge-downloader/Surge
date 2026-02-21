package peer

import (
	"context"
	"fmt"
	"net"
	"time"
)

const (
	peerDialTimeout     = 3 * time.Second
	peerHandshakeWindow = 5 * time.Second
	peerKeepAlivePeriod = 15 * time.Second
	peerReadTimeout     = 45 * time.Second
	peerKeepAliveSend   = 30 * time.Second
	peerSocketBuffer    = 4 << 20
)

type Session struct {
	conn                net.Conn
	supportsExtProtocol bool
	readTimeout         time.Duration
	keepAliveSend       time.Duration
}

func NewFromConn(conn net.Conn, supportsExtProtocol bool) *Session {
	return NewFromConnWithConfig(conn, supportsExtProtocol, peerReadTimeout, peerKeepAliveSend)
}

func NewFromConnWithConfig(conn net.Conn, supportsExtProtocol bool, readTimeout time.Duration, keepAliveSend time.Duration) *Session {
	tuneTCPConn(conn)
	return &Session{
		conn:                conn,
		supportsExtProtocol: supportsExtProtocol,
		readTimeout:         normalizeDuration(readTimeout, peerReadTimeout),
		keepAliveSend:       normalizeDuration(keepAliveSend, peerKeepAliveSend),
	}
}

func Dial(ctx context.Context, addr net.TCPAddr, infoHash [20]byte, peerID [20]byte) (*Session, error) {
	return DialWithConfig(ctx, addr, infoHash, peerID, peerReadTimeout, peerKeepAliveSend)
}

func DialWithConfig(ctx context.Context, addr net.TCPAddr, infoHash [20]byte, peerID [20]byte, readTimeout time.Duration, keepAliveSend time.Duration) (*Session, error) {
	dialer := net.Dialer{Timeout: peerDialTimeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr.String())
	if err != nil {
		return nil, err
	}
	tuneTCPConn(conn)
	_ = conn.SetDeadline(time.Now().Add(peerHandshakeWindow))
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
	return &Session{
		conn:                conn,
		supportsExtProtocol: hs.SupportsExtensionProtocol(),
		readTimeout:         normalizeDuration(readTimeout, peerReadTimeout),
		keepAliveSend:       normalizeDuration(keepAliveSend, peerKeepAliveSend),
	}, nil
}

func (s *Session) Close() error {
	if s.conn == nil {
		return nil
	}
	return s.conn.Close()
}

func (s *Session) SupportsExtensionProtocol() bool {
	if s == nil {
		return false
	}
	return s.supportsExtProtocol
}

func (s *Session) ReadTimeout() time.Duration {
	if s == nil {
		return peerReadTimeout
	}
	return normalizeDuration(s.readTimeout, peerReadTimeout)
}

func (s *Session) KeepAliveSendInterval() time.Duration {
	if s == nil {
		return peerKeepAliveSend
	}
	return normalizeDuration(s.keepAliveSend, peerKeepAliveSend)
}

func tuneTCPConn(conn net.Conn) {
	tcp, ok := conn.(*net.TCPConn)
	if !ok {
		return
	}
	_ = tcp.SetNoDelay(true)
	_ = tcp.SetKeepAlive(true)
	_ = tcp.SetKeepAlivePeriod(peerKeepAlivePeriod)
	_ = tcp.SetReadBuffer(peerSocketBuffer)
	_ = tcp.SetWriteBuffer(peerSocketBuffer)
}

func normalizeDuration(v time.Duration, fallback time.Duration) time.Duration {
	if v <= 0 {
		return fallback
	}
	return v
}
