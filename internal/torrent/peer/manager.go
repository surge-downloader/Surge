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

	maxPeers int
	mu       sync.Mutex
	active   map[string]*Session
}

func NewManager(infoHash [20]byte, peerID [20]byte, maxPeers int) *Manager {
	if maxPeers <= 0 {
		maxPeers = 32
	}
	return &Manager{
		infoHash: infoHash,
		peerID:   peerID,
		maxPeers: maxPeers,
		active:   make(map[string]*Session),
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

	m.mu.Lock()
	m.active[key] = sess
	m.mu.Unlock()
}

func (m *Manager) CloseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, s := range m.active {
		_ = s.Close()
		delete(m.active, k)
	}
}

func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.active)
}
