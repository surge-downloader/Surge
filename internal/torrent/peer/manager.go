package peer

import (
	"context"
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

	maxPeers int
	mu       sync.Mutex
	active   map[string]*Conn
}

func NewManager(infoHash [20]byte, peerID [20]byte, picker Picker, layout PieceLayout, store Storage, maxPeers int) *Manager {
	if maxPeers <= 0 {
		maxPeers = 32
	}
	return &Manager{
		infoHash: infoHash,
		peerID:   peerID,
		picker:   picker,
		layout:   layout,
		store:    store,
		maxPeers: maxPeers,
		active:   make(map[string]*Conn),
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
				m.tryDial(ctx, addr)
			}
		}
	}()
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
	conn := NewConn(sess, addr, m.picker, m.layout, m.store, pipe)
	conn.Start(ctx)

	m.mu.Lock()
	m.active[key] = conn
	m.mu.Unlock()
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, s := range m.active {
		_ = s.sess.Close()
		delete(m.active, k)
	}
}

func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.active)
}
