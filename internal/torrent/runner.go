package torrent

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/torrent/peer"
)

type Runner struct {
	meta       *TorrentMeta
	layout     *FileLayout
	picker     *PiecePicker
	session    *Session
	peers      *peer.Manager
	listenAddr *net.TCPAddr
}

func NewRunner(meta *TorrentMeta, baseDir string, cfg SessionConfig, state *types.ProgressState) (*Runner, error) {
	layout, err := NewFileLayout(baseDir, meta.Info)
	if err != nil {
		return nil, err
	}
	totalPieces := int((layout.TotalLength + meta.Info.PieceLength - 1) / meta.Info.PieceLength)
	picker := NewPiecePicker(totalPieces)
	sess := NewSession(meta.InfoHash, flattenTrackers(meta), cfg)
	store := peer.Storage(layout)
	var progressStore *ProgressStore
	if state != nil {
		progressStore = NewProgressStore(layout, state)
		store = progressStore
	}
	mgr := peer.NewManager(meta.InfoHash, sess.peerID, picker, layout, store, cfg.MaxPeers, cfg.UploadSlots, cfg.RequestPipeline, peer.ManagerConfig{
		HealthEnabled:          cfg.HealthEnabled,
		LowRateCullFactor:      cfg.LowRateCullFactor,
		HealthMinUptime:        cfg.HealthMinUptime,
		HealthCullMaxPerTick:   cfg.HealthCullMaxPerTick,
		HealthRedialBlock:      cfg.HealthRedialBlock,
		EvictionCooldown:       cfg.EvictionCooldown,
		EvictionMinUptime:      cfg.EvictionMinUptime,
		IdleEvictionThreshold:  cfg.IdleEvictionThreshold,
		EvictionKeepRateMinBps: cfg.EvictionKeepRateMinBps,
		PeerReadTimeout:        cfg.PeerReadTimeout,
		PeerKeepaliveSend:      cfg.PeerKeepaliveSend,
	})
	if progressStore != nil {
		progressStore.SetOnVerified(func(pieceIndex int) {
			mgr.BroadcastHave(pieceIndex)
			sess.AddDownloaded(layout.PieceSize(int64(pieceIndex)))
		})

		// On resume, verify we communicate already completed pieces to the picker.
		// Without this, the piece picker thinks everything is incomplete and double-requests.
		for i := 0; i < totalPieces; i++ {
			if progressStore.HasPiece(int64(i)) {
				picker.Done(i)
			}
		}
	}

	return &Runner{
		meta:    meta,
		layout:  layout,
		picker:  picker,
		session: sess,
		peers:   mgr,
	}, nil
}

func (r *Runner) Start(ctx context.Context) {
	if r.layout != nil {
		go func() {
			<-ctx.Done()
			_ = r.layout.Close()
		}()
	}

	if r.peers != nil && r.session != nil {
		if addr, err := r.peers.StartInbound(ctx, r.session.cfg.ListenAddr); err == nil && addr != nil {
			r.listenAddr = addr
			r.session.SetListenPort(addr.Port)
		} else if addr, err := r.peers.StartInbound(ctx, "0.0.0.0:0"); err == nil && addr != nil {
			// Fallback to an ephemeral port if fixed bind fails.
			r.listenAddr = addr
			r.session.SetListenPort(addr.Port)
		}
	}
	peerCh := r.session.DiscoverPeers(ctx)
	r.peers.Start(ctx, peerCh)
	r.session.SetLowPeerMode(true)
	go r.monitorPeerPressure(ctx)
}

func flattenTrackers(meta *TorrentMeta) []string {
	var out []string
	if meta.Announce != "" {
		out = append(out, meta.Announce)
	}
	for _, tier := range meta.AnnounceList {
		out = append(out, tier...)
	}
	return out
}

func (r *Runner) Wait(ctx context.Context) error {
	// placeholder: no completion tracking yet
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(2 * time.Second):
		return fmt.Errorf("runner incomplete")
	}
}

func (r *Runner) ListenAddr() *net.TCPAddr {
	if r == nil || r.listenAddr == nil {
		return nil
	}
	return r.listenAddr
}

func (r *Runner) ActivePeerCount() int {
	if r == nil || r.peers == nil {
		return 0
	}
	return r.peers.Count()
}

func (r *Runner) PeerStats() peer.Stats {
	if r == nil || r.peers == nil {
		return peer.Stats{}
	}
	return r.peers.Stats()
}

func (r *Runner) monitorPeerPressure(ctx context.Context) {
	if r == nil || r.peers == nil || r.session == nil {
		return
	}
	lowPeerThreshold := computeLowPeerThreshold(r.session.cfg.MaxPeers)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			active := r.peers.Count()
			r.session.SetLowPeerMode(active < lowPeerThreshold)
		}
	}
}

func computeLowPeerThreshold(maxPeers int) int {
	if maxPeers <= 0 {
		maxPeers = defaultSessionMaxPeers
	}
	threshold := maxPeers / 3
	if threshold < 8 {
		threshold = 8
	}
	if threshold > 48 {
		threshold = 48
	}
	return threshold
}
