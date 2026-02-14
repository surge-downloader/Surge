package torrent

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/surge-downloader/surge/internal/torrent/dht"
	"github.com/surge-downloader/surge/internal/torrent/tracker"
)

type SessionConfig struct {
	ListenAddr      string
	BootstrapNodes  []string
	TrackerInterval time.Duration
	TotalLength     int64
	MaxPeers        int
	UploadSlots     int
	RequestPipeline int
}

type PeerSource interface {
	Start(ctx context.Context) <-chan net.TCPAddr
}

type Session struct {
	infoHash   [20]byte
	trackers   []string
	cfg        SessionConfig
	peerID     [20]byte
	mu         sync.Mutex
	listenPort int
}

func NewSession(infoHash [20]byte, trackers []string, cfg SessionConfig) *Session {
	if cfg.TrackerInterval == 0 {
		cfg.TrackerInterval = 10 * time.Second
	}
	if cfg.TotalLength <= 0 {
		cfg.TotalLength = 1
	}
	return &Session{
		infoHash: infoHash,
		trackers: trackers,
		cfg:      cfg,
		peerID:   tracker.DefaultPeerID(),
	}
}

// DiscoverPeers merges tracker and DHT peer streams.
func (s *Session) DiscoverPeers(ctx context.Context) <-chan net.TCPAddr {
	out := make(chan net.TCPAddr, 256)

	// tracker stream
	go func() {
		defer close(out)
		seen := make(map[string]bool)
		tick := time.NewTicker(s.cfg.TrackerInterval)
		defer tick.Stop()
		started := true

		for {
			for _, tr := range s.trackers {
				resp, err := tracker.Announce(tr, tracker.AnnounceRequest{
					InfoHash: s.infoHash,
					PeerID:   s.peerID,
					Port:     s.announcePort(),
					Left:     s.cfg.TotalLength,
					Event:    startedEvent(started),
					NumWant:  50,
				})
				if err == nil && resp != nil {
					for _, p := range resp.Peers {
						addr := net.TCPAddr{IP: p.IP, Port: p.Port}
						key := addr.String()
						if seen[key] {
							continue
						}
						seen[key] = true
						select {
						case out <- addr:
						case <-ctx.Done():
							return
						}
					}
				}
			}
			started = false
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()

	// dht stream
	go func() {
		svc, err := dht.NewService(dht.ServiceConfig{
			ListenAddr: s.cfg.ListenAddr,
			Bootstrap:  s.cfg.BootstrapNodes,
		})
		if err != nil {
			return
		}
		defer func() { _ = svc.Close() }()

		seen := make(map[string]bool)
		for p := range svc.DiscoverPeers(ctx, s.infoHash) {
			addr := net.TCPAddr{IP: p.IP, Port: p.Port}
			key := addr.String()
			if seen[key] {
				continue
			}
			seen[key] = true
			select {
			case out <- addr:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out
}

func (s *Session) SetListenPort(port int) {
	if port <= 0 {
		return
	}
	s.mu.Lock()
	s.listenPort = port
	s.mu.Unlock()
}

func (s *Session) announcePort() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listenPort > 0 {
		return s.listenPort
	}
	return 6881
}

func startedEvent(initial bool) string {
	if initial {
		return "started"
	}
	return ""
}
