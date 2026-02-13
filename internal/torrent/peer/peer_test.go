package peer

import (
	"bytes"
	"context"
	"net"
	"testing"
)

func TestHandshakeRoundTrip(t *testing.T) {
	var ih [20]byte
	var pid [20]byte
	copy(ih[:], []byte("12345678901234567890"))
	copy(pid[:], []byte("ABCDEFGHIJKLMNOPQRST"))

	var buf bytes.Buffer
	if err := WriteHandshake(&buf, Handshake{InfoHash: ih, PeerID: pid}); err != nil {
		t.Fatalf("write err: %v", err)
	}
	got, err := ReadHandshake(&buf)
	if err != nil {
		t.Fatalf("read err: %v", err)
	}
	if got.InfoHash != ih || got.PeerID != pid {
		t.Fatalf("handshake mismatch")
	}
}

func TestMessageReadWrite(t *testing.T) {
	var buf bytes.Buffer
	msg := &Message{ID: MsgInterested}
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("write err: %v", err)
	}
	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("read err: %v", err)
	}
	if got.ID != MsgInterested {
		t.Fatalf("msg id mismatch")
	}
}

func TestDialHandshakeSelf(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen err: %v", err)
	}
	defer func() { _ = l.Close() }()

	var ih [20]byte
	var pid [20]byte
	copy(ih[:], []byte("12345678901234567890"))
	copy(pid[:], []byte("ABCDEFGHIJKLMNOPQRST"))

	done := make(chan struct{})
	go func() {
		conn, _ := l.Accept()
		defer func() { _ = conn.Close() }()
		_, _ = ReadHandshake(conn)
		_ = WriteHandshake(conn, Handshake{InfoHash: ih, PeerID: pid})
		close(done)
	}()

	addr := l.Addr().(*net.TCPAddr)
	s, err := Dial(context.Background(), *addr, ih, pid)
	if err != nil {
		t.Fatalf("dial err: %v", err)
	}
	_ = s.Close()
	<-done
}
