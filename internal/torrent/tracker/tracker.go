package tracker

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"strings"
	"time"

	atracker "github.com/anacrolix/torrent/tracker"
	"github.com/anacrolix/torrent/types"
	"github.com/anacrolix/torrent/types/infohash"
)

type FailureKind int

const (
	FailureUnknown FailureKind = iota
	FailureTimeout
	FailureDNS
	FailureRefused
	FailureUnreachable
)

type AnnounceRequest struct {
	InfoHash   [20]byte
	PeerID     [20]byte
	Port       int
	Uploaded   int64
	Downloaded int64
	Left       int64
	Event      string
	NumWant    int
}

type AnnounceResponse struct {
	Interval int
	Peers    []Peer
}

type Peer struct {
	IP   net.IP
	Port int
}

func Announce(announceURL string, req AnnounceRequest) (*AnnounceResponse, error) {
	var lastErr error
	const maxAttempts = 2
	for attempt := 0; attempt < maxAttempts; attempt++ {
		resp, err := announceOnce(announceURL, req)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		if !isTimeoutErr(err) || attempt+1 >= maxAttempts {
			break
		}
		time.Sleep(time.Duration(300*(attempt+1)) * time.Millisecond)
	}
	return nil, lastErr
}

func announceOnce(announceURL string, req AnnounceRequest) (*AnnounceResponse, error) {
	areq := atracker.AnnounceRequest{
		InfoHash:   infohash.T(req.InfoHash),
		PeerId:     types.PeerID(req.PeerID),
		Downloaded: req.Downloaded,
		Left:       req.Left,
		Uploaded:   req.Uploaded,
		Event:      mapEvent(req.Event),
		NumWant:    int32(req.NumWant),
		Port:       uint16(req.Port),
	}
	if areq.NumWant <= 0 {
		areq.NumWant = -1
	}
	if req.Port < 0 || req.Port > 65535 {
		areq.Port = 0
	}

	res, err := atracker.Announce{
		TrackerUrl: announceURL,
		Request:    areq,
		Context:    context.Background(),
	}.Do()
	if err != nil {
		return nil, err
	}

	out := &AnnounceResponse{
		Interval: int(res.Interval),
		Peers:    make([]Peer, 0, len(res.Peers)),
	}
	for _, p := range res.Peers {
		out.Peers = append(out.Peers, Peer{
			IP:   p.IP,
			Port: p.Port,
		})
	}
	return out, nil
}

func mapEvent(event string) atracker.AnnounceEvent {
	switch strings.ToLower(strings.TrimSpace(event)) {
	case "started":
		return atracker.Started
	case "stopped":
		return atracker.Stopped
	case "completed":
		return atracker.Completed
	default:
		return atracker.None
	}
}

func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) {
		return ne.Timeout()
	}
	type timeout interface{ Timeout() bool }
	var te timeout
	if errors.As(err, &te) {
		return te.Timeout()
	}
	return false
}

func ClassifyFailure(err error) FailureKind {
	if err == nil {
		return FailureUnknown
	}
	if isTimeoutErr(err) {
		return FailureTimeout
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return FailureDNS
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "no such host"), strings.Contains(msg, "server misbehaving"):
		return FailureDNS
	case strings.Contains(msg, "connection refused"):
		return FailureRefused
	case strings.Contains(msg, "network is unreachable"), strings.Contains(msg, "host is unreachable"), strings.Contains(msg, "no route to host"):
		return FailureUnreachable
	default:
		return FailureUnknown
	}
}

func DefaultPeerID() [20]byte {
	const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	var id [20]byte
	copy(id[:8], []byte("-SG0001-"))
	for i := 8; i < 20; i++ {
		id[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return id
}
