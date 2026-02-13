package dht

import (
	"errors"
	"math/rand"
	"net"
	"sync"
	"time"
)

type Config struct {
	ListenAddr   string
	Bootstrap    []string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

type Node struct {
	id      NodeID
	addr    *net.UDPAddr
	conn    *net.UDPConn
	cfg     Config
	mu      sync.Mutex
	pending map[string]chan *Message
	closed  chan struct{}
}

func New(cfg Config) (*Node, error) {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "0.0.0.0:0"
	}
	if cfg.ReadTimeout == 0 {
		cfg.ReadTimeout = 5 * time.Second
	}
	if cfg.WriteTimeout == 0 {
		cfg.WriteTimeout = 5 * time.Second
	}
	addr, err := net.ResolveUDPAddr("udp", cfg.ListenAddr)
	if err != nil {
		return nil, err
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, err
	}
	id, err := NewNodeID()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	n := &Node{
		id:      id,
		addr:    conn.LocalAddr().(*net.UDPAddr),
		conn:    conn,
		cfg:     cfg,
		pending: make(map[string]chan *Message),
		closed:  make(chan struct{}),
	}
	go n.readLoop()
	return n, nil
}

func (n *Node) Close() error {
	close(n.closed)
	return n.conn.Close()
}

func (n *Node) ID() NodeID {
	return n.id
}

func (n *Node) LocalAddr() net.Addr {
	return n.conn.LocalAddr()
}

func (n *Node) Bootstrap() error {
	if len(n.cfg.Bootstrap) == 0 {
		return nil
	}
	for _, host := range n.cfg.Bootstrap {
		addr, err := net.ResolveUDPAddr("udp", host)
		if err != nil {
			continue
		}
		_, _ = n.Ping(addr)
	}
	return nil
}

func (n *Node) Ping(addr *net.UDPAddr) (*Message, error) {
	tid := n.newTID()
	msg := &Message{
		T: tid,
		Y: krpcQuery,
		Q: "ping",
		A: map[string]any{
			"id": n.id[:],
		},
	}
	return n.exchange(addr, msg)
}

func (n *Node) FindNode(addr *net.UDPAddr, target NodeID) (*Message, error) {
	tid := n.newTID()
	msg := &Message{
		T: tid,
		Y: krpcQuery,
		Q: "find_node",
		A: map[string]any{
			"id":     n.id[:],
			"target": target[:],
		},
	}
	return n.exchange(addr, msg)
}

func (n *Node) GetPeers(addr *net.UDPAddr, infoHash [20]byte) (*Message, error) {
	tid := n.newTID()
	msg := &Message{
		T: tid,
		Y: krpcQuery,
		Q: "get_peers",
		A: map[string]any{
			"id":        n.id[:],
			"info_hash": infoHash[:],
		},
	}
	return n.exchange(addr, msg)
}

func (n *Node) AnnouncePeer(addr *net.UDPAddr, infoHash [20]byte, token string, port int) (*Message, error) {
	tid := n.newTID()
	msg := &Message{
		T: tid,
		Y: krpcQuery,
		Q: "announce_peer",
		A: map[string]any{
			"id":        n.id[:],
			"info_hash": infoHash[:],
			"token":     []byte(token),
			"port":      int64(port),
		},
	}
	return n.exchange(addr, msg)
}

func (n *Node) exchange(addr *net.UDPAddr, msg *Message) (*Message, error) {
	ch := make(chan *Message, 1)
	n.mu.Lock()
	n.pending[msg.T] = ch
	n.mu.Unlock()

	defer func() {
		n.mu.Lock()
		delete(n.pending, msg.T)
		n.mu.Unlock()
	}()

	data, err := EncodeMessage(msg)
	if err != nil {
		return nil, err
	}
	_ = n.conn.SetWriteDeadline(time.Now().Add(n.cfg.WriteTimeout))
	if _, err := n.conn.WriteToUDP(data, addr); err != nil {
		return nil, err
	}
	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(n.cfg.ReadTimeout):
		return nil, errors.New("dht timeout")
	}
}

func (n *Node) readLoop() {
	buf := make([]byte, 4096)
	for {
		select {
		case <-n.closed:
			return
		default:
		}
		n.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		nr, addr, err := n.conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			continue
		}
		msg, err := DecodeMessage(buf[:nr])
		if err != nil || msg.T == "" {
			continue
		}
		n.mu.Lock()
		ch := n.pending[msg.T]
		n.mu.Unlock()
		if ch != nil {
			select {
			case ch <- msg:
			default:
			}
			continue
		}
		// handle incoming query minimal (ping)
		if msg.Y == krpcQuery && msg.Q == "ping" {
			resp := &Message{
				T: msg.T,
				Y: krpcResponse,
				R: map[string]any{
					"id": n.id[:],
				},
			}
			data, err := EncodeMessage(resp)
			if err == nil {
				_, _ = n.conn.WriteToUDP(data, addr)
			}
		}
	}
}

func (n *Node) newTID() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	b := []byte{alphabet[rand.Intn(len(alphabet))], alphabet[rand.Intn(len(alphabet))]}
	return string(b)
}
