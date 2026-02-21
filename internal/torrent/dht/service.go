package dht

import (
	"context"
	"net"
	"time"

	adht "github.com/anacrolix/dht/v2"
)

type Service struct {
	server *adht.Server
}

type Peer struct {
	IP   net.IP
	Port int
}

type ServiceConfig struct {
	ListenAddr string
	Bootstrap  []string
}

func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = "0.0.0.0:0"
	}

	pc, err := net.ListenPacket("udp4", cfg.ListenAddr)
	if err != nil {
		return nil, err
	}

	sc := adht.NewDefaultServerConfig()
	sc.Conn = pc
	if len(cfg.Bootstrap) > 0 {
		bootstrap := append([]string(nil), cfg.Bootstrap...)
		sc.StartingNodes = func() ([]adht.Addr, error) {
			return adht.ResolveHostPorts(bootstrap)
		}
	}

	server, err := adht.NewServer(sc)
	if err != nil {
		_ = pc.Close()
		return nil, err
	}
	return &Service{server: server}, nil
}

func (s *Service) Close() error {
	if s == nil || s.server == nil {
		return nil
	}
	s.server.Close()
	return nil
}

// DiscoverPeers periodically traverses DHT and emits deduplicated peers.
func (s *Service) DiscoverPeers(ctx context.Context, infoHash [20]byte) <-chan Peer {
	ch := make(chan Peer, 256)
	go func() {
		defer close(ch)
		if s == nil || s.server == nil {
			return
		}
		seen := make(map[string]bool)
		repeat := time.NewTicker(8 * time.Second)
		defer repeat.Stop()

		run := func() bool {
			a, err := s.server.AnnounceTraversal(infoHash)
			if err != nil {
				return false
			}
			defer a.Close()

			for {
				select {
				case <-ctx.Done():
					a.StopTraversing()
					return true
				case values, ok := <-a.Peers:
					if !ok {
						return false
					}
					for _, p := range values.Peers {
						key := p.String()
						if key == "" || seen[key] {
							continue
						}
						seen[key] = true
						select {
						case ch <- Peer{IP: p.IP, Port: p.Port}:
						case <-ctx.Done():
							a.StopTraversing()
							return true
						}
					}
				}
			}
		}

		for {
			if stop := run(); stop {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-repeat.C:
			}
		}
	}()
	return ch
}
