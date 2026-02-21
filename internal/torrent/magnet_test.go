package torrent

import "testing"

func TestParseMagnetHex(t *testing.T) {
	m, err := ParseMagnet("magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=test")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if !m.InfoHashOK {
		t.Fatalf("missing infohash")
	}
	if m.DisplayName != "test" {
		t.Fatalf("unexpected name: %s", m.DisplayName)
	}
}

func TestParseMagnetBase32(t *testing.T) {
	m, err := ParseMagnet("magnet:?xt=urn:btih:AERUKZ4JVPG66AJDIVTYTK6N54ASGRLH")
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if !m.InfoHashOK {
		t.Fatalf("missing infohash")
	}
}
