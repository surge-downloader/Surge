package dht

import (
	"context"
	"sync"
	"time"
)

type Service struct {
	node      *Node
	bootstrap []string
	mu        sync.Mutex
}

type ServiceConfig struct {
	ListenAddr string
	Bootstrap  []string
}

func NewService(cfg ServiceConfig) (*Service, error) {
	n, err := New(Config{
		ListenAddr: cfg.ListenAddr,
		Bootstrap:  cfg.Bootstrap,
	})
	if err != nil {
		return nil, err
	}
	return &Service{
		node:      n,
		bootstrap: cfg.Bootstrap,
	}, nil
}

func (s *Service) Close() error {
	return s.node.Close()
}

// DiscoverPeers periodically queries DHT for peers of infoHash.
// It emits peers on the returned channel until ctx is done.
func (s *Service) DiscoverPeers(ctx context.Context, infoHash [20]byte) <-chan Peer {
	ch := make(chan Peer, 256)
	go func() {
		defer close(ch)
		_ = s.node.Bootstrap()
		seen := make(map[string]bool)
		ticker := time.NewTicker(8 * time.Second)
		defer ticker.Stop()

		for {
			peers, _ := s.node.GetPeersIterative(infoHash, 64)
			for _, p := range peers {
				key := p.IP.String()
				if key == "" {
					continue
				}
				key = key + ":" + itoa(p.Port)
				if seen[key] {
					continue
				}
				seen[key] = true
				select {
				case ch <- p:
				case <-ctx.Done():
					return
				}
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
	return ch
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [12]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
