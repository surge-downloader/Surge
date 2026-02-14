package torrent

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/surge-downloader/surge/internal/torrent/peer"
)

func TestSessionPeerIDStable(t *testing.T) {
	s := NewSession([20]byte{}, nil, SessionConfig{})
	if s.peerID == ([20]byte{}) {
		t.Fatalf("peerID not set")
	}
}

func TestManagerStartStop(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := make(chan net.TCPAddr, 1)
	ch <- net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 1}
	close(ch)

	m := peerManagerForTest()
	m.Start(ctx, ch)
	time.Sleep(10 * time.Millisecond)
	m.CloseAll()
}

func peerManagerForTest() *peer.Manager {
	var ih [20]byte
	var pid [20]byte
	return peer.NewManager(ih, pid, nil, nil, nil, 1, 0, 8)
}
