package source

import "testing"

func TestIsSupportedRejectsMagnet(t *testing.T) {
	raw := "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"
	if !IsMagnet(raw) {
		t.Fatalf("expected magnet parser to recognize magnet link")
	}
	if IsSupported(raw) {
		t.Fatalf("expected magnet links to be unsupported until metadata fetch is implemented")
	}
}

func TestIsSupportedAcceptsHTTPAndTorrent(t *testing.T) {
	if !IsSupported("https://example.com/file.bin") {
		t.Fatalf("expected http url to be supported")
	}
	if !IsSupported("https://example.com/file.torrent") {
		t.Fatalf("expected torrent url to be supported")
	}
}
