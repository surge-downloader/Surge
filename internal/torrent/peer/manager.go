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

	maxPeers    int
	uploadSlots int
	mu          sync.Mutex
	active      map[string]*Conn
	uploading   map[string]bool
	unchoked    int
}

func NewManager(infoHash [20]byte, peerID [20]byte, picker Picker, layout PieceLayout, store Storage, maxPeers int, uploadSlots int) *Manager {
	if maxPeers <= 0 {
		maxPeers = 32
	}
	if uploadSlots < 0 {
		uploadSlots = 0
	}
	return &Manager{
		infoHash:    infoHash,
		peerID:      peerID,
		picker:      picker,
		layout:      layout,
		store:       store,
		maxPeers:    maxPeers,
		uploadSlots: uploadSlots,
		active:      make(map[string]*Conn),
		uploading:   make(map[string]bool),
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
	conn := NewConn(sess, addr, m.picker, m.layout, m.store, pipe, func() {
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
	for k, s := range m.active {
		_ = s.sess.Close()
		delete(m.active, k)
		delete(m.uploading, k)
	}
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
