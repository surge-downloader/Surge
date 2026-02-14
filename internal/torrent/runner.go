package torrent

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/surge-downloader/surge/internal/torrent/peer"
)

type Runner struct {
	meta    *TorrentMeta
	layout  *FileLayout
	picker  *PiecePicker
	session *Session
	peers   *peer.Manager
}

func NewRunner(meta *TorrentMeta, baseDir string, cfg SessionConfig) (*Runner, error) {
	layout, err := NewFileLayout(baseDir, meta.Info)
	if err != nil {
		return nil, err
	}
	totalPieces := int((layout.TotalLength + meta.Info.PieceLength - 1) / meta.Info.PieceLength)
	picker := NewPiecePicker(totalPieces)
	sess := NewSession(meta.InfoHash, flattenTrackers(meta), cfg)
	mgr := peer.NewManager(meta.InfoHash, sess.peerID, picker, layout, layout, 32)

	return &Runner{
		meta:    meta,
		layout:  layout,
		picker:  picker,
		session: sess,
		peers:   mgr,
	}, nil
}

func (r *Runner) Start(ctx context.Context) {
	peerCh := r.session.DiscoverPeers(ctx)
	r.peers.Start(ctx, peerCh)
}

func flattenTrackers(meta *TorrentMeta) []string {
	var out []string
	if meta.Announce != "" {
		out = append(out, meta.Announce)
	}
	for _, tier := range meta.AnnounceList {
		for _, tr := range tier {
			out = append(out, tr)
		}
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
	// placeholder for future incoming peers
	return nil
}
