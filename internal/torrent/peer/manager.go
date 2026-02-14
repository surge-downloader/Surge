package peer

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

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
	pending         map[string]bool
	uploading       map[string]bool
	unchoked        int
	listener        net.Listener
	dialSem         chan struct{}
}

func NewManager(infoHash [20]byte, peerID [20]byte, picker Picker, layout PieceLayout, store Storage, maxPeers int, uploadSlots int, requestPipeline int) *Manager {
	if maxPeers <= 0 {
		maxPeers = 32
	}
	if uploadSlots < 0 {
		uploadSlots = 0
	}
	if requestPipeline <= 0 {
		requestPipeline = 8
	}
	dialWorkers := maxPeers
	if dialWorkers < 8 {
		dialWorkers = 8
	}
	if dialWorkers > 64 {
		dialWorkers = 64
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
		pending:         make(map[string]bool),
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
				m.tryDialAsync(ctx, addr)
			}
		}
	}()
}

func (m *Manager) tryDialAsync(ctx context.Context, addr net.TCPAddr) {
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
	if m.pending[key] {
		m.mu.Unlock()
		return
	}
	m.pending[key] = true
	m.mu.Unlock()

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
	m.mu.Unlock()

	dialCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	sess, err := Dial(dialCtx, addr, m.infoHash, m.peerID)
	if err != nil {
		return
	}

	var pipe Pipeline
	conn := NewConn(sess, addr, m.picker, m.layout, m.store, pipe, m.requestPipeline, func() {
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
	m.mu.Unlock()

	var pipe Pipeline
	sess := NewFromConn(raw)
	conn := NewConn(sess, *addr, m.picker, m.layout, m.store, pipe, m.requestPipeline, func() {
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
	m.unchoked = 0
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
