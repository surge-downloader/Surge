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

const (
	defaultTrackerInterval   = 5 * time.Second
	defaultSessionMaxPeers   = 128
	peerDiscoverBufferSize   = 1024
	peerRetryWindow          = 8 * time.Second
	trackerRetryBackoffStart = 1 * time.Second
	trackerRetryBackoffMax   = 30 * time.Second
	minTrackerInterval       = 3 * time.Second
	maxLowPeerInterval       = 10 * time.Second
)

type SessionConfig struct {
	ListenAddr      string
	BootstrapNodes  []string
	TrackerInterval time.Duration
	TotalLength     int64
	MaxPeers        int
	UploadSlots     int
	RequestPipeline int

	HealthEnabled          bool
	LowRateCullFactor      float64
	HealthMinUptime        time.Duration
	HealthCullMaxPerTick   int
	HealthRedialBlock      time.Duration
	EvictionCooldown       time.Duration
	EvictionMinUptime      time.Duration
	IdleEvictionThreshold  time.Duration
	EvictionKeepRateMinBps int64
	PeerReadTimeout        time.Duration
	PeerKeepaliveSend      time.Duration
	TrackerIntervalNormal  time.Duration
	TrackerIntervalLowPeer time.Duration
	TrackerNumWantNormal   int
	TrackerNumWantLowPeer  int
	LSDEnabled             bool
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

var fallbackTrackers = []string{
	"udp://tracker.opentrackr.org:1337/announce",
	"udp://open.stealth.si:80/announce",
	"udp://tracker.torrent.eu.org:451/announce",
	"udp://tracker.moeking.me:6969/announce",
	"https://tracker.opentrackr.org:443/announce",
}

func NewSession(infoHash [20]byte, trackers []string, cfg SessionConfig) *Session {
	if cfg.TrackerInterval == 0 {
		cfg.TrackerInterval = defaultTrackerInterval
	}
	if cfg.TrackerIntervalNormal <= 0 {
		cfg.TrackerIntervalNormal = cfg.TrackerInterval
	}
	if cfg.TrackerIntervalLowPeer <= 0 {
		cfg.TrackerIntervalLowPeer = minTrackerInterval
	}
	if cfg.TrackerNumWantNormal <= 0 {
		cfg.TrackerNumWantNormal = 256
	}
	if cfg.TrackerNumWantLowPeer <= 0 {
		cfg.TrackerNumWantLowPeer = 300
	}
	if cfg.TotalLength <= 0 {
		cfg.TotalLength = 1
	}
	if cfg.MaxPeers <= 0 {
		cfg.MaxPeers = defaultSessionMaxPeers
	}
	if !cfg.HealthEnabled && cfg.LowRateCullFactor == 0 && cfg.HealthMinUptime == 0 {
		cfg.HealthEnabled = true
	}
	return &Session{
		infoHash: infoHash,
		trackers: withFallbackTrackers(trackers),
		cfg:      cfg,
		peerID:   tracker.DefaultPeerID(),
	}
}

// DiscoverPeers merges tracker and DHT peer streams.
func (s *Session) DiscoverPeers(ctx context.Context) <-chan net.TCPAddr {
	out := make(chan net.TCPAddr, peerDiscoverBufferSize)
	var producers sync.WaitGroup
	producers.Add(3)

	// tracker stream
	go func() {
		defer producers.Done()
		seen := make(map[string]time.Time)
		var seenMu sync.Mutex
		emitPeer := func(addr net.TCPAddr) bool {
			key := addr.String()
			seenMu.Lock()
			ok := shouldEmitPeer(seen, key, peerRetryWindow)
			seenMu.Unlock()
			return ok
		}

		var trackerWG sync.WaitGroup
		for _, tr := range s.trackers {
			announceURL := tr
			trackerWG.Add(1)
			go func() {
				defer trackerWG.Done()

				started := true
				backoff := time.Duration(0)
				for {
					resp, err := tracker.Announce(announceURL, tracker.AnnounceRequest{
						InfoHash: s.infoHash,
						PeerID:   s.peerID,
						Port:     s.announcePort(),
						Left:     s.cfg.TotalLength,
						Event:    startedEvent(started),
						NumWant:  s.trackerNumWant(),
					})

					wait := s.currentTrackerInterval()
					if err != nil {
						if backoff <= 0 {
							backoff = trackerRetryBackoffStart
						} else {
							backoff *= 2
							if backoff > trackerRetryBackoffMax {
								backoff = trackerRetryBackoffMax
							}
						}
						wait = backoff
						utils.Debug("Tracker announce failed (%s): %v (next retry in %s)", announceURL, err, backoff)
					} else {
						backoff = 0
						if resp != nil && resp.Interval > 0 {
							trackerNext := time.Duration(resp.Interval) * time.Second
							if trackerNext < minTrackerInterval {
								trackerNext = minTrackerInterval
							}
							if trackerNext > 10*time.Minute {
								trackerNext = 10 * time.Minute
							}
							// Aggressive mode: do not let sparse tracker intervals slow peer refresh.
							// Use the faster cadence between our configured target and tracker suggestion.
							if trackerNext < wait {
								wait = trackerNext
							}
						}

						if resp != nil {
							for _, p := range resp.Peers {
								addr := net.TCPAddr{IP: p.IP, Port: p.Port}
								if !emitPeer(addr) {
									continue
								}
								select {
								case out <- addr:
								case <-ctx.Done():
									return
								}
							}
						}
					}

					started = false
					if wait < time.Second {
						wait = time.Second
					}
					timer := time.NewTimer(wait)
					select {
					case <-ctx.Done():
						timer.Stop()
						return
					case <-timer.C:
					}
				}
			}()
		}
		trackerWG.Wait()
	}()

	// dht stream
	go func() {
		defer producers.Done()
		svc, err := dht.NewService(dht.ServiceConfig{
			ListenAddr: s.cfg.ListenAddr,
			Bootstrap:  s.cfg.BootstrapNodes,
		})
		if err != nil {
			return
		}
		defer func() { _ = svc.Close() }()

		seen := make(map[string]time.Time)
		for p := range svc.DiscoverPeers(ctx, s.infoHash) {
			addr := net.TCPAddr{IP: p.IP, Port: p.Port}
			key := addr.String()
			if !shouldEmitPeer(seen, key, peerRetryWindow) {
				continue
			}
			select {
			case out <- addr:
			case <-ctx.Done():
				return
			}
		}
	}()

	// local service discovery (BEP14) stream
	go func() {
		defer producers.Done()
		if !s.cfg.LSDEnabled {
			return
		}
		seen := make(map[string]time.Time)
		for addr := range discoverLocalPeers(ctx, s.infoHash, s.announcePort()) {
			key := addr.String()
			if !shouldEmitPeer(seen, key, peerRetryWindow) {
				continue
			}
			select {
			case out <- addr:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		producers.Wait()
		close(out)
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
	base := s.cfg.TrackerIntervalNormal
	if base <= 0 {
		base = defaultTrackerInterval
	}
	if s.lowPeerMode {
		low := s.cfg.TrackerIntervalLowPeer
		if low <= 0 {
			low = minTrackerInterval
		}
		if low < minTrackerInterval {
			low = minTrackerInterval
		}
		if low > maxLowPeerInterval {
			low = maxLowPeerInterval
		}
		return low
	}
	return base
}

func (s *Session) trackerNumWant() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	target := s.cfg.TrackerNumWantNormal
	if target <= 0 {
		target = 256
	}
	if s.lowPeerMode {
		low := s.cfg.TrackerNumWantLowPeer
		if low <= 0 {
			low = 300
		}
		target = low
	}
	if target < 50 {
		target = 50
	}
	if target > 1000 {
		target = 1000
	}
	return target
}

func startedEvent(initial bool) string {
	if initial {
		return "started"
	}
	return ""
}

func withFallbackTrackers(trackers []string) []string {
	seen := make(map[string]bool, len(trackers)+len(fallbackTrackers))
	out := make([]string, 0, len(trackers)+len(fallbackTrackers))
	for _, tr := range trackers {
		if tr == "" || seen[tr] {
			continue
		}
		seen[tr] = true
		out = append(out, tr)
	}
	for _, tr := range fallbackTrackers {
		if seen[tr] {
			continue
		}
		seen[tr] = true
		out = append(out, tr)
	}
	return out
}

func shouldEmitPeer(seen map[string]time.Time, key string, retryWindow time.Duration) bool {
	now := time.Now()
	last, ok := seen[key]
	if ok && now.Sub(last) < retryWindow {
		return false
	}
	seen[key] = now
	return true
}
