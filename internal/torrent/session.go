package torrent

import (
	"context"
	"net"
	"sync"
	"time"

	"github.com/surge-downloader/surge/internal/torrent/dht"
	"github.com/surge-downloader/surge/internal/torrent/tracker"
	"github.com/surge-downloader/surge/internal/utils"
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
	infoHash    [20]byte
	trackers    []string
	cfg         SessionConfig
	peerID      [20]byte
	mu          sync.Mutex
	listenPort  int
	lowPeerMode bool
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
		type trackerRetryState struct {
			next    time.Time
			backoff time.Duration
		}
		retry := make(map[string]trackerRetryState)
		started := true

		for {
			now := time.Now()
			minNext := now.Add(s.currentTrackerInterval())

			for _, tr := range s.trackers {
				st := retry[tr]
				if !st.next.IsZero() && now.Before(st.next) {
					if st.next.Before(minNext) {
						minNext = st.next
					}
					continue
				}

				resp, err := tracker.Announce(tr, tracker.AnnounceRequest{
					InfoHash: s.infoHash,
					PeerID:   s.peerID,
					Port:     s.announcePort(),
					Left:     s.cfg.TotalLength,
					Event:    startedEvent(started),
					NumWant:  50,
				})
				if err != nil {
					if st.backoff <= 0 {
						st.backoff = 2 * time.Second
					} else {
						st.backoff *= 2
						if st.backoff > 2*time.Minute {
							st.backoff = 2 * time.Minute
						}
					}
					st.next = now.Add(st.backoff)
					retry[tr] = st
					if st.next.Before(minNext) {
						minNext = st.next
					}
					utils.Debug("Tracker announce failed (%s): %v (next retry in %s)", tr, err, st.backoff)
					continue
				}

				st.backoff = 0
				next := s.currentTrackerInterval()
				if resp != nil && resp.Interval > 0 {
					trackerNext := time.Duration(resp.Interval) * time.Second
					if trackerNext < 5*time.Second {
						trackerNext = 5 * time.Second
					}
					if trackerNext > 10*time.Minute {
						trackerNext = 10 * time.Minute
					}
					if !s.isLowPeerMode() {
						next = trackerNext
					}
				}
				st.next = now.Add(next)
				retry[tr] = st
				if st.next.Before(minNext) {
					minNext = st.next
				}

				if resp == nil {
					continue
				}
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

			started = false
			wait := time.Until(minNext)
			if wait < 1*time.Second {
				wait = 1 * time.Second
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
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

func (s *Session) SetLowPeerMode(low bool) {
	s.mu.Lock()
	s.lowPeerMode = low
	s.mu.Unlock()
}

func (s *Session) isLowPeerMode() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lowPeerMode
}

func (s *Session) currentTrackerInterval() time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	base := s.cfg.TrackerInterval
	if base <= 0 {
		base = 10 * time.Second
	}
	if s.lowPeerMode {
		if base > 15*time.Second {
			return 15 * time.Second
		}
		if base < 5*time.Second {
			return 5 * time.Second
		}
	}
	return base
}

func startedEvent(initial bool) string {
	if initial {
		return "started"
	}
	return ""
}
