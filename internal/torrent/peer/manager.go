package peer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/surge-downloader/surge/internal/torrent/peer/health"
)

const (
	defaultMaxPeers         = 128
	defaultRequestPipeline  = 32
	minDialWorkers          = 16
	maxDialWorkers          = 192
	managerDialTimeout      = 3 * time.Second
	maintainInterval        = 2 * time.Second
	defaultEvictionCooldown = 5 * time.Second
	defaultEvictionUptime   = 20 * time.Second
	defaultIdleThreshold    = 45 * time.Second
	defaultEvictionMinRate  = 512 * 1024
	defaultLowRateCull      = 0.3
	defaultHealthRedial     = 2 * time.Minute
	defaultHealthCullMax    = 2
	defaultPeerReadTimeout  = 45 * time.Second
	defaultKeepAliveSend    = 30 * time.Second

	// Compatibility aliases for tests.
	evictionCooldown        = defaultEvictionCooldown
	minEvictionUptime       = defaultEvictionUptime
	idleEvictionThreshold   = defaultIdleThreshold
	evictionKeepRateMinimum = defaultEvictionMinRate
	lowRateCullFactor       = defaultLowRateCull
	healthRedialBlock       = defaultHealthRedial
	healthCullMaxPerTick    = defaultHealthCullMax
	dialBackoffBase         = 15 * time.Second
	dialBackoffMax          = 10 * time.Minute
)

type dialRetryState struct {
	failures    int
	nextAttempt time.Time
}

type ManagerConfig struct {
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
}

type Manager struct {
	infoHash [20]byte
	peerID   [20]byte
	picker   Picker
	layout   PieceLayout
	store    Storage

	maxPeers        int
	uploadSlots     int
	requestPipeline int
	mu              sync.Mutex
	active          map[string]*Conn
	discovered      map[string]bool
	pending         map[string]bool
	retry           map[string]dialRetryState
	uploading       map[string]bool
	unchoked        int
	listener        net.Listener
	dialSem         chan struct{}
	dialAttempts    int
	dialSuccess     int
	dialFailures    int
	inboundAccepted int
	lastEvictAt     time.Time

	healthEnabled          bool
	lowRateCullFactor      float64
	healthMinUptime        time.Duration
	healthCullMaxPerTick   int
	healthRedialBlock      time.Duration
	evictionCooldown       time.Duration
	evictionMinUptime      time.Duration
	idleEvictionThreshold  time.Duration
	evictionKeepRateMinBps float64
	peerReadTimeout        time.Duration
	peerKeepaliveSend      time.Duration
}

type Stats struct {
	Discovered      int
	Pending         int
	Active          int
	DialAttempts    int
	DialSuccess     int
	DialFailures    int
	InboundAccepted int
}

func NewManager(infoHash [20]byte, peerID [20]byte, picker Picker, layout PieceLayout, store Storage, maxPeers int, uploadSlots int, requestPipeline int, cfg ManagerConfig) *Manager {
	if maxPeers <= 0 {
		maxPeers = defaultMaxPeers
	}
	if uploadSlots < 0 {
		uploadSlots = 0
	}
	if requestPipeline <= 0 {
		requestPipeline = defaultRequestPipeline
	}
	dialWorkers := maxPeers
	if dialWorkers < minDialWorkers {
		dialWorkers = minDialWorkers
	}
	if dialWorkers > maxDialWorkers {
		dialWorkers = maxDialWorkers
	}
	healthEnabled := cfg.HealthEnabled
	if !healthEnabled && cfg.LowRateCullFactor == 0 && cfg.HealthMinUptime == 0 && cfg.HealthCullMaxPerTick == 0 && cfg.HealthRedialBlock == 0 {
		healthEnabled = true
	}
	lowRateCullFactor := cfg.LowRateCullFactor
	if lowRateCullFactor <= 0 {
		lowRateCullFactor = defaultLowRateCull
	}
	if lowRateCullFactor > 1 {
		lowRateCullFactor = 1
	}
	healthCullMax := cfg.HealthCullMaxPerTick
	if healthCullMax <= 0 {
		healthCullMax = defaultHealthCullMax
	}
	if healthCullMax > 16 {
		healthCullMax = 16
	}
	evictionMinRate := cfg.EvictionKeepRateMinBps
	if evictionMinRate <= 0 {
		evictionMinRate = defaultEvictionMinRate
	}
	return &Manager{
		infoHash:        infoHash,
		peerID:          peerID,
		picker:          picker,
		layout:          layout,
		store:           store,
		maxPeers:        maxPeers,
		uploadSlots:     uploadSlots,
		requestPipeline: requestPipeline,
		active:          make(map[string]*Conn),
		discovered:      make(map[string]bool),
		pending:         make(map[string]bool),
		retry:           make(map[string]dialRetryState),
		uploading:       make(map[string]bool),
		dialSem:         make(chan struct{}, dialWorkers),

		healthEnabled:          healthEnabled,
		lowRateCullFactor:      lowRateCullFactor,
		healthMinUptime:        withDefaultDuration(cfg.HealthMinUptime, defaultEvictionUptime),
		healthCullMaxPerTick:   healthCullMax,
		healthRedialBlock:      withDefaultDuration(cfg.HealthRedialBlock, defaultHealthRedial),
		evictionCooldown:       withDefaultDuration(cfg.EvictionCooldown, defaultEvictionCooldown),
		evictionMinUptime:      withDefaultDuration(cfg.EvictionMinUptime, defaultEvictionUptime),
		idleEvictionThreshold:  withDefaultDuration(cfg.IdleEvictionThreshold, defaultIdleThreshold),
		evictionKeepRateMinBps: float64(evictionMinRate),
		peerReadTimeout:        withDefaultDuration(cfg.PeerReadTimeout, defaultPeerReadTimeout),
		peerKeepaliveSend:      withDefaultDuration(cfg.PeerKeepaliveSend, defaultKeepAliveSend),
	}
}

func (m *Manager) Start(ctx context.Context, peers <-chan net.TCPAddr) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				m.CloseAll()
				return
			case addr, ok := <-peers:
				if !ok {
					m.CloseAll()
					return
				}
				m.markDiscovered(addr.String())
				m.tryDialAsync(ctx, addr)
			}
		}
	}()
	go m.maintain(ctx)
}

func (m *Manager) markDiscovered(key string) {
	m.mu.Lock()
	if key != "" {
		m.discovered[key] = true
	}
	m.mu.Unlock()
}

func (m *Manager) tryDialAsync(ctx context.Context, addr net.TCPAddr) {
	key := addr.String()
	var victim *Conn
	now := time.Now()
	m.mu.Lock()
	if len(m.active) >= m.maxPeers {
		victim = m.pickEvictionCandidateLocked()
		if victim == nil {
			m.mu.Unlock()
			return
		}
	}
	if _, ok := m.active[key]; ok {
		m.mu.Unlock()
		return
	}
	if m.pending[key] {
		m.mu.Unlock()
		return
	}
	if !m.shouldAttemptDialLocked(key, now) {
		m.mu.Unlock()
		return
	}
	m.pending[key] = true
	m.mu.Unlock()
	if victim != nil {
		victim.Close()
	}

	select {
	case m.dialSem <- struct{}{}:
	case <-ctx.Done():
		m.mu.Lock()
		delete(m.pending, key)
		m.mu.Unlock()
		return
	}

	go func() {
		defer func() {
			<-m.dialSem
			m.mu.Lock()
			delete(m.pending, key)
			m.mu.Unlock()
		}()
		m.tryDial(ctx, addr)
	}()
}

func (m *Manager) StartInbound(ctx context.Context, listenAddr string) (*net.TCPAddr, error) {
	if listenAddr == "" {
		listenAddr = "0.0.0.0:0"
	}
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return nil, err
	}

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		return nil, fmt.Errorf("unexpected listener addr type %T", ln.Addr())
	}

	m.mu.Lock()
	if m.listener != nil {
		_ = m.listener.Close()
	}
	m.listener = ln
	m.mu.Unlock()

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					time.Sleep(100 * time.Millisecond)
					continue
				}
				return
			}
			go m.acceptInboundConn(ctx, conn)
		}
	}()

	return addr, nil
}

func (m *Manager) tryDial(ctx context.Context, addr net.TCPAddr) {
	key := addr.String()
	m.mu.Lock()
	if len(m.active) >= m.maxPeers {
		m.mu.Unlock()
		return
	}
	if _, ok := m.active[key]; ok {
		m.mu.Unlock()
		return
	}
	m.dialAttempts++
	m.mu.Unlock()

	dialCtx, cancel := context.WithTimeout(ctx, managerDialTimeout)
	defer cancel()
	sess, err := DialWithConfig(dialCtx, addr, m.infoHash, m.peerID, m.peerReadTimeout, m.peerKeepaliveSend)
	if err != nil {
		m.mu.Lock()
		m.dialFailures++
		m.noteDialFailureLocked(key, time.Now())
		m.mu.Unlock()
		return
	}
	m.mu.Lock()
	m.dialSuccess++
	m.noteDialSuccessLocked(key)
	m.mu.Unlock()

	var pipe Pipeline
	conn := NewConn(sess, addr, m.picker, m.layout, m.store, pipe, m.requestPipeline, func(pexAddr net.TCPAddr) {
		m.onPEXPeer(ctx, pexAddr)
	}, func() {
		m.onClose(key)
	})
	conn.Start(ctx)

	allowUpload := false
	m.mu.Lock()
	m.active[key] = conn
	if m.uploadSlots > 0 && m.unchoked < m.uploadSlots {
		m.unchoked++
		m.uploading[key] = true
		allowUpload = true
	}
	m.mu.Unlock()

	if allowUpload {
		conn.SetChoke(false)
	}
}

func (m *Manager) acceptInboundConn(ctx context.Context, raw net.Conn) {
	_ = raw.SetDeadline(time.Now().Add(8 * time.Second))
	hs, err := ReadHandshake(raw)
	if err != nil {
		_ = raw.Close()
		return
	}
	if hs.InfoHash != m.infoHash {
		_ = raw.Close()
		return
	}
	if err := WriteHandshake(raw, Handshake{InfoHash: m.infoHash, PeerID: m.peerID}); err != nil {
		_ = raw.Close()
		return
	}
	_ = raw.SetDeadline(time.Time{})

	addr, ok := raw.RemoteAddr().(*net.TCPAddr)
	if !ok {
		_ = raw.Close()
		return
	}
	key := addr.String()

	m.mu.Lock()
	if len(m.active) >= m.maxPeers {
		m.mu.Unlock()
		_ = raw.Close()
		return
	}
	if _, exists := m.active[key]; exists {
		m.mu.Unlock()
		_ = raw.Close()
		return
	}
	m.inboundAccepted++
	m.noteDialSuccessLocked(key)
	m.mu.Unlock()

	var pipe Pipeline
	sess := NewFromConnWithConfig(raw, hs.SupportsExtensionProtocol(), m.peerReadTimeout, m.peerKeepaliveSend)
	conn := NewConn(sess, *addr, m.picker, m.layout, m.store, pipe, m.requestPipeline, func(pexAddr net.TCPAddr) {
		m.onPEXPeer(ctx, pexAddr)
	}, func() {
		m.onClose(key)
	})
	conn.Start(ctx)

	allowUpload := false
	m.mu.Lock()
	m.active[key] = conn
	if m.uploadSlots > 0 && m.unchoked < m.uploadSlots {
		m.unchoked++
		m.uploading[key] = true
		allowUpload = true
	}
	m.mu.Unlock()

	if allowUpload {
		conn.SetChoke(false)
	}
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.listener != nil {
		_ = m.listener.Close()
		m.listener = nil
	}
	for k, s := range m.active {
		_ = s.sess.Close()
		delete(m.active, k)
		delete(m.uploading, k)
	}
	clear(m.pending)
	clear(m.retry)
	m.unchoked = 0
}

func (m *Manager) maintain(ctx context.Context) {
	ticker := time.NewTicker(maintainInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now()
			m.mu.Lock()
			victim := m.pickEvictionCandidateLocked()
			stragglers := m.collectHealthEvictionsLocked()

			toClose := make([]*Conn, 0, 1+len(stragglers))
			if victim != nil {
				toClose = append(toClose, victim)
			}
			for _, key := range stragglers {
				conn, ok := m.active[key]
				if !ok || conn == nil {
					continue
				}
				m.noteHealthEvictionLocked(key, now)
				toClose = append(toClose, conn)
			}
			m.mu.Unlock()

			seen := make(map[*Conn]struct{}, len(toClose))
			for _, c := range toClose {
				if c == nil {
					continue
				}
				if _, ok := seen[c]; ok {
					continue
				}
				seen[c] = struct{}{}
				c.Close()
			}
		}
	}
}

func (m *Manager) collectHealthEvictionsLocked() []string {
	healthEnabled := m.healthEnabled
	if !healthEnabled && m.lowRateCullFactor == 0 && m.healthMinUptime == 0 && m.healthCullMaxPerTick == 0 {
		healthEnabled = true
	}
	if !healthEnabled {
		return nil
	}
	if m.maxPeers > 0 && len(m.active) < m.maxPeers {
		// Do not churn peers while we still have room to grow.
		return nil
	}
	if len(m.active) < 2 {
		return nil
	}
	samples := make([]health.PeerSample, 0, len(m.active))
	for key, c := range m.active {
		p := c.Performance()
		samples = append(samples, health.PeerSample{
			Key:     key,
			RateBps: p.RateBps,
			Uptime:  p.Uptime,
		})
	}
	minUptime := withDefaultDuration(m.healthMinUptime, defaultEvictionUptime)
	factor := m.lowRateCullFactor
	if factor <= 0 {
		factor = defaultLowRateCull
	}
	if factor > 1 {
		factor = 1
	}
	maxCull := m.healthCullMaxPerTick
	if maxCull <= 0 {
		maxCull = defaultHealthCullMax
	}
	keys := health.BelowRelativeMean(samples, minUptime, factor)
	if len(keys) > maxCull {
		keys = keys[:maxCull]
	}
	return keys
}

func (m *Manager) pickEvictionCandidateLocked() *Conn {
	if len(m.active) < m.maxPeers {
		return nil
	}
	evictionCooldown := withDefaultDuration(m.evictionCooldown, defaultEvictionCooldown)
	if !m.lastEvictAt.IsZero() && time.Since(m.lastEvictAt) < evictionCooldown {
		return nil
	}
	minUptime := withDefaultDuration(m.evictionMinUptime, defaultEvictionUptime)
	idleThreshold := withDefaultDuration(m.idleEvictionThreshold, defaultIdleThreshold)
	minKeepRate := m.evictionKeepRateMinBps
	if minKeepRate <= 0 {
		minKeepRate = defaultEvictionMinRate
	}

	var victim *Conn
	bestRate := 1e18
	for _, c := range m.active {
		p := c.Performance()
		if p.Uptime < minUptime {
			continue
		}
		if p.IdleFor > idleThreshold {
			victim = c
			bestRate = -1
			break
		}
		if p.RateBps < bestRate {
			bestRate = p.RateBps
			victim = c
		}
	}
	if victim == nil {
		return nil
	}
	// Avoid evicting decent peers.
	if bestRate > minKeepRate && bestRate != -1 {
		return nil
	}
	m.lastEvictAt = time.Now()
	return victim
}

func (m *Manager) Stats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Stats{
		Discovered:      len(m.discovered),
		Pending:         len(m.pending),
		Active:          len(m.active),
		DialAttempts:    m.dialAttempts,
		DialSuccess:     m.dialSuccess,
		DialFailures:    m.dialFailures,
		InboundAccepted: m.inboundAccepted,
	}
}

func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.active)
}

func (m *Manager) BroadcastHave(pieceIndex int) {
	m.mu.Lock()
	conns := make([]*Conn, 0, len(m.active))
	for _, c := range m.active {
		conns = append(conns, c)
	}
	m.mu.Unlock()

	for _, c := range conns {
		c.SendHave(pieceIndex)
	}
}

func (m *Manager) onClose(key string) {
	m.mu.Lock()
	if m.uploading[key] {
		m.unchoked--
		delete(m.uploading, key)
	}
	delete(m.active, key)
	m.mu.Unlock()
}

func (m *Manager) onPEXPeer(ctx context.Context, addr net.TCPAddr) {
	if addr.Port <= 0 || addr.Port > 65535 || addr.IP == nil || addr.IP.IsUnspecified() {
		return
	}
	m.markDiscovered(addr.String())
	m.tryDialAsync(ctx, addr)
}

func (m *Manager) shouldAttemptDialLocked(key string, now time.Time) bool {
	if key == "" {
		return false
	}
	st, ok := m.retry[key]
	if !ok {
		return true
	}
	return !now.Before(st.nextAttempt)
}

func (m *Manager) noteDialSuccessLocked(key string) {
	if key == "" {
		return
	}
	delete(m.retry, key)
}

func (m *Manager) noteDialFailureLocked(key string, now time.Time) {
	if key == "" {
		return
	}
	st := m.retry[key]
	st.failures++
	st.nextAttempt = now.Add(dialBackoffDuration(st.failures))
	m.retry[key] = st
}

func (m *Manager) noteHealthEvictionLocked(key string, now time.Time) {
	if key == "" {
		return
	}
	st := m.retry[key]
	block := withDefaultDuration(m.healthRedialBlock, defaultHealthRedial)
	until := now.Add(block)
	if st.nextAttempt.Before(until) {
		st.nextAttempt = until
	}
	if st.failures < 1 {
		st.failures = 1
	}
	m.retry[key] = st
}

func dialBackoffDuration(failures int) time.Duration {
	if failures <= 0 {
		return dialBackoffBase
	}
	backoff := dialBackoffBase
	for i := 1; i < failures; i++ {
		if backoff >= dialBackoffMax {
			return dialBackoffMax
		}
		backoff *= 2
		if backoff >= dialBackoffMax {
			return dialBackoffMax
		}
	}
	return backoff
}

func withDefaultDuration(v time.Duration, fallback time.Duration) time.Duration {
	if v <= 0 {
		return fallback
	}
	return v
}
