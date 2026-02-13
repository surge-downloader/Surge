package tracker

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/surge-downloader/surge/internal/torrent/bencode"
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

func AnnounceHTTP(announceURL string, req AnnounceRequest) (*AnnounceResponse, error) {
	u, err := url.Parse(announceURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported tracker scheme: %s", u.Scheme)
	}

	q := u.Query()
	q.Set("info_hash", string(req.InfoHash[:]))
	q.Set("peer_id", string(req.PeerID[:]))
	q.Set("port", strconv.Itoa(req.Port))
	q.Set("uploaded", strconv.FormatInt(req.Uploaded, 10))
	q.Set("downloaded", strconv.FormatInt(req.Downloaded, 10))
	q.Set("left", strconv.FormatInt(req.Left, 10))
	q.Set("compact", "1")
	q.Set("numwant", strconv.Itoa(req.NumWant))
	if req.Event != "" {
		q.Set("event", req.Event)
	}

	u.RawQuery = q.Encode()
	resp, err := http.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("tracker error: %s - %s", resp.Status, strings.TrimSpace(string(body)))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	val, err := bencode.Decode(data)
	if err != nil {
		return nil, err
	}
	m, ok := val.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid tracker response")
	}
	if failure, ok := m["failure reason"]; ok {
		if b, ok := failure.([]byte); ok {
			return nil, fmt.Errorf("tracker failure: %s", string(b))
		}
	}

	var out AnnounceResponse
	if v, ok := m["interval"].(int64); ok {
		out.Interval = int(v)
	}

	switch peers := m["peers"].(type) {
	case []byte:
		out.Peers = parseCompactPeers(peers)
	case []any:
		out.Peers = parsePeerList(peers)
	}

	return &out, nil
}

func parseCompactPeers(b []byte) []Peer {
	if len(b)%6 != 0 {
		return nil
	}
	out := make([]Peer, 0, len(b)/6)
	for i := 0; i < len(b); i += 6 {
		ip := net.IPv4(b[i], b[i+1], b[i+2], b[i+3])
		port := int(binary.BigEndian.Uint16(b[i+4 : i+6]))
		out = append(out, Peer{IP: ip, Port: port})
	}
	return out
}

func parsePeerList(list []any) []Peer {
	var out []Peer
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		ipb, _ := m["ip"].([]byte)
		portv, _ := m["port"].(int64)
		if len(ipb) == 0 || portv == 0 {
			continue
		}
		out = append(out, Peer{
			IP:   net.ParseIP(string(ipb)),
			Port: int(portv),
		})
	}
	return out
}
