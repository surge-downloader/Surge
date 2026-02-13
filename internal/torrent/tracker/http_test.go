package tracker

import (
	"bytes"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/surge-downloader/surge/internal/torrent/bencode"
)

func TestAnnounceHTTP_Compact(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peers := []byte{
			127, 0, 0, 1, 0x1F, 0x90, // 8080
		}
		resp := map[string]any{
			"interval": int64(1800),
			"peers":    peers,
		}
		b, _ := bencode.Encode(resp)
		_, _ = w.Write(b)
	}))
	defer s.Close()

	req := AnnounceRequest{
		InfoHash: [20]byte{1, 2, 3},
		PeerID:   [20]byte{4, 5, 6},
		Port:     6881,
		Left:     10,
		NumWant:  50,
	}
	resp, err := AnnounceHTTP(s.URL, req)
	if err != nil {
		t.Fatalf("announce failed: %v", err)
	}
	if resp.Interval != 1800 {
		t.Fatalf("interval mismatch")
	}
	if len(resp.Peers) != 1 {
		t.Fatalf("expected 1 peer")
	}
	if !resp.Peers[0].IP.Equal(net.IPv4(127, 0, 0, 1)) || resp.Peers[0].Port != 8080 {
		t.Fatalf("peer mismatch")
	}
}

func TestAnnounceHTTP_List(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"interval": int64(120),
			"peers": []any{
				map[string]any{"ip": []byte("127.0.0.1"), "port": int64(6881)},
			},
		}
		b, _ := bencode.Encode(resp)
		_, _ = w.Write(b)
	}))
	defer s.Close()

	req := AnnounceRequest{
		InfoHash: [20]byte{1, 2, 3},
		PeerID:   [20]byte{4, 5, 6},
		Port:     6881,
		Left:     10,
		NumWant:  50,
	}
	resp, err := AnnounceHTTP(s.URL, req)
	if err != nil {
		t.Fatalf("announce failed: %v", err)
	}
	if resp.Interval != 120 || len(resp.Peers) != 1 {
		t.Fatalf("unexpected response")
	}
}

func TestParseCompactPeers(t *testing.T) {
	buf := bytes.NewBuffer(nil)
	buf.Write([]byte{1, 2, 3, 4, 0x1A, 0xE1})
	peers := parseCompactPeers(buf.Bytes())
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer")
	}
	if peers[0].Port != 6881 {
		t.Fatalf("port mismatch")
	}
}
