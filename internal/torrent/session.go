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
	trackerLoopMinWait       = 250 * time.Millisecond
	trackerInitialFollowup   = 800 * time.Millisecond
	trackerHealthyWindow     = 2 * time.Minute
	trackerDemoteMinWait     = 2 * time.Minute
	trackerDemoteMaxWait     = 10 * time.Minute
	trackerDemoteAfter       = 3
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
		trackerSuccess := make(map[string]time.Time)
		var trackerStateMu sync.Mutex
		emitPeer := func(addr net.TCPAddr) bool {
			key := addr.String()
			seenMu.Lock()
			ok := shouldEmitPeer(seen, key, peerRetryWindow)
			seenMu.Unlock()
			return ok
		}
		markTrackerSuccess := func(url string) {
			trackerStateMu.Lock()
			trackerSuccess[url] = time.Now()
			trackerStateMu.Unlock()
		}
		hasOtherHealthyTracker := func(url string) bool {
			now := time.Now()
			trackerStateMu.Lock()
			defer trackerStateMu.Unlock()
			for tr, last := range trackerSuccess {
				if tr == url {
					continue
				}
				if now.Sub(last) <= trackerHealthyWindow {
					return true
				}
			}
			return false
		}

		var trackerWG sync.WaitGroup
		for _, tr := range s.trackers {
			announceURL := tr
			trackerWG.Add(1)
			go func() {
				defer trackerWG.Done()

				started := true
				failureStreak := 0
				for {
					firstAnnounce := started
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
						failureStreak++
						wait = trackerFailureWait(err, failureStreak, hasOtherHealthyTracker(announceURL))
						utils.Debug("Tracker announce failed (%s): %v [kind=%s] (next retry in %s)", announceURL, err, trackerFailureKindString(tracker.ClassifyFailure(err)), wait)
					} else {
						failureStreak = 0
						markTrackerSuccess(announceURL)
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
								if !isPublicRoutablePeer(addr.IP) {
									continue
								}
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

					if firstAnnounce && err == nil && wait > trackerInitialFollowup {
						wait = trackerInitialFollowup
					}
					started = false
					if wait < trackerLoopMinWait {
						wait = trackerLoopMinWait
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
			if !isPublicRoutablePeer(addr.IP) {
				continue
			}
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

func trackerFailureWait(err error, failureStreak int, hasHealthyAlternatives bool) time.Duration {
	if failureStreak < 1 {
		failureStreak = 1
	}

	backoff := trackerRetryBackoffStart
	for i := 1; i < failureStreak; i++ {
		backoff *= 2
		if backoff >= trackerRetryBackoffMax {
			backoff = trackerRetryBackoffMax
			break
		}
	}

	kind := tracker.ClassifyFailure(err)
	switch kind {
	case tracker.FailureDNS, tracker.FailureRefused, tracker.FailureUnreachable:
		if backoff < 15*time.Second {
			backoff = 15 * time.Second
		}
		if hasHealthyAlternatives && failureStreak >= trackerDemoteAfter {
			if backoff < trackerDemoteMinWait {
				backoff = trackerDemoteMinWait
			}
			if backoff > trackerDemoteMaxWait {
				backoff = trackerDemoteMaxWait
			}
		}
	case tracker.FailureTimeout:
		if hasHealthyAlternatives && failureStreak >= trackerDemoteAfter {
			if backoff < 45*time.Second {
				backoff = 45 * time.Second
			}
		}
	default:
	}

	if backoff > trackerDemoteMaxWait {
		backoff = trackerDemoteMaxWait
	}
	if backoff < time.Second {
		backoff = time.Second
	}
	return backoff
}

func trackerFailureKindString(kind tracker.FailureKind) string {
	switch kind {
	case tracker.FailureTimeout:
		return "timeout"
	case tracker.FailureDNS:
		return "dns"
	case tracker.FailureRefused:
		return "refused"
	case tracker.FailureUnreachable:
		return "unreachable"
	default:
		return "unknown"
	}
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

func isPublicRoutablePeer(ip net.IP) bool {
	if ip == nil {
		return false
	}
	ip = ip.To16()
	if ip == nil {
		return false
	}
	if ip.IsUnspecified() || ip.IsLoopback() || ip.IsMulticast() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return false
	}
	if isCarrierGradeNAT(ip) {
		return false
	}
	return true
}

func isCarrierGradeNAT(ip net.IP) bool {
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	// 100.64.0.0/10
	return v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127
}
