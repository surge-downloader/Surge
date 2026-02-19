package torrent

import (
	"net"
	"strings"
	"testing"
)

func TestMakeLSDAnnounce(t *testing.T) {
	var infoHash [20]byte
	copy(infoHash[:], []byte("12345678901234567890"))

	msg := string(makeLSDAnnounce(infoHash, 51413))
	if !strings.Contains(msg, "BT-SEARCH * HTTP/1.1") {
		t.Fatalf("announce missing start line: %q", msg)
	}
	if !strings.Contains(msg, "Port: 51413") {
		t.Fatalf("announce missing port header: %q", msg)
	}
	if !strings.Contains(msg, "Infohash: "+percentEncodeInfoHash(infoHash)) {
		t.Fatalf("announce missing infohash header: %q", msg)
	}
}

func TestParseLSDAnnounceValid(t *testing.T) {
	var infoHash [20]byte
	copy(infoHash[:], []byte("12345678901234567890"))

	msg := makeLSDAnnounce(infoHash, 6881)
	from := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 2), Port: 49000}
	addr, ok := parseLSDAnnounce(msg, from, infoHash)
	if !ok {
		t.Fatal("expected valid lsd packet")
	}
	if addr.String() != "10.0.0.2:6881" {
		t.Fatalf("unexpected parsed addr: %s", addr.String())
	}
}

func TestParseLSDAnnounceRejectsWrongInfoHash(t *testing.T) {
	var expected [20]byte
	copy(expected[:], []byte("aaaaaaaaaaaaaaaaaaaa"))
	var other [20]byte
	copy(other[:], []byte("bbbbbbbbbbbbbbbbbbbb"))

	msg := makeLSDAnnounce(other, 6881)
	from := &net.UDPAddr{IP: net.IPv4(10, 0, 0, 3), Port: 49000}
	if _, ok := parseLSDAnnounce(msg, from, expected); ok {
		t.Fatal("expected parser to reject mismatched infohash")
	}
}
