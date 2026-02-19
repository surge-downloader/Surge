package peer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	defaultMaxPeers         = 128
	defaultRequestPipeline  = 32
	minDialWorkers          = 16
	maxDialWorkers          = 192
	managerDialTimeout      = 3 * time.Second
	maintainInterval        = 2 * time.Second
	evictionCooldown        = 3 * time.Second
	minEvictionUptime       = 8 * time.Second
	idleEvictionThreshold   = 10 * time.Second
	evictionKeepRateMinimum = 512 * 1024
	dialBackoffBase         = 15 * time.Second
	dialBackoffMax          = 10 * time.Minute
)

type dialRetryState struct {
	failures    int
	nextAttempt time.Time
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

func NewManager(infoHash [20]byte, peerID [20]byte, picker Picker, layout PieceLayout, store Storage, maxPeers int, uploadSlots int, requestPipeline int) *Manager {
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
	sess, err := Dial(dialCtx, addr, m.infoHash, m.peerID)
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
	sess := NewFromConn(raw, hs.SupportsExtensionProtocol())
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
			m.mu.Lock()
			victim := m.pickEvictionCandidateLocked()
			m.mu.Unlock()
			if victim != nil {
				victim.Close()
			}
		}
	}
}

func (m *Manager) pickEvictionCandidateLocked() *Conn {
	if len(m.active) < m.maxPeers {
		return nil
	}
	if !m.lastEvictAt.IsZero() && time.Since(m.lastEvictAt) < evictionCooldown {
		return nil
	}

	var victim *Conn
	bestRate := 1e18
	for _, c := range m.active {
		p := c.Performance()
		if p.Uptime < minEvictionUptime {
			continue
		}
		if p.IdleFor > idleEvictionThreshold {
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
	if bestRate > evictionKeepRateMinimum && bestRate != -1 {
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
