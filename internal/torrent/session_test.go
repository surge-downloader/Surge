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

func TestDiscoverPeersChannelClosesOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// Keep tracker list empty to avoid network tracker announces in test.
	s := &Session{
		trackers: nil,
		cfg: SessionConfig{
			ListenAddr: "invalid-listen-addr",
		},
	}

	out := s.DiscoverPeers(ctx)
	cancel()

	deadline := time.After(2 * time.Second)
	for {
		select {
		case _, ok := <-out:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("discover peers channel did not close after cancellation")
		}
	}
}

func peerManagerForTest() *peer.Manager {
	var ih [20]byte
	var pid [20]byte
	return peer.NewManager(ih, pid, nil, nil, nil, 1, 0, 8, peer.ManagerConfig{})
}

func TestTrackerFailureWaitDemotesDNSWhenAlternativesHealthy(t *testing.T) {
	err := &net.DNSError{Err: "no such host"}
	wait := trackerFailureWait(err, trackerDemoteAfter, true)
	if wait < trackerDemoteMinWait {
		t.Fatalf("expected dns failure demotion >= %s, got %s", trackerDemoteMinWait, wait)
	}
}

func TestTrackerFailureWaitDoesNotHardDemoteDNSWithoutHealthyAlternatives(t *testing.T) {
	err := &net.DNSError{Err: "no such host"}
	wait := trackerFailureWait(err, trackerDemoteAfter, false)
	if wait >= trackerDemoteMinWait {
		t.Fatalf("expected dns failure wait below hard demotion without healthy alternatives, got %s", wait)
	}
}

func TestTrackerFailureWaitTimeoutDemotesModerately(t *testing.T) {
	wait := trackerFailureWait(context.DeadlineExceeded, trackerDemoteAfter+1, true)
	if wait < 45*time.Second {
		t.Fatalf("expected timeout demotion to be at least 45s, got %s", wait)
	}
	if wait >= trackerDemoteMinWait {
		t.Fatalf("expected timeout demotion to stay below hard demotion window, got %s", wait)
	}
}
