package torrent

import (
	"bytes"
	"crypto/sha1"
	"testing"

	"github.com/anacrolix/torrent/bencode"
)

func TestParseTorrent_InfoHash(t *testing.T) {
	info := map[string]any{
		"name":         "file.txt",
		"piece length": int64(16384),
		"length":       int64(5),
		"pieces":       []byte("12345678901234567890"),
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatalf("encode info failed: %v", err)
	}
	root := map[string]any{
		"announce": "http://tracker",
		"info":     info,
	}
	rootBytes, err := bencode.Marshal(root)
	if err != nil {
		t.Fatalf("encode root failed: %v", err)
	}

	meta, err := ParseTorrent(rootBytes)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	want := sha1.Sum(infoBytes)
	if meta.InfoHash != want {
		t.Fatalf("infohash mismatch")
	}
	if meta.Info.Name != "file.txt" {
		t.Fatalf("unexpected name: %s", meta.Info.Name)
	}
}

func TestParseTorrent_Multifile(t *testing.T) {
	info := map[string]any{
		"name":         "dir",
		"piece length": int64(16384),
		"pieces":       []byte("1234567890123456789012345678901234567890"),
		"files": []any{
			map[string]any{
				"length": int64(3),
				"path":   []any{[]byte("a.txt")},
			},
			map[string]any{
				"length": int64(4),
				"path":   []any{[]byte("b.txt")},
			},
		},
	}
	infoBytes, _ := bencode.Marshal(info)
	root := map[string]any{
		"announce": "http://tracker",
		"info":     info,
	}
	rootBytes, _ := bencode.Marshal(root)

	meta, err := ParseTorrent(rootBytes)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if meta.Info.TotalLength() != 7 {
		t.Fatalf("unexpected total length")
	}
	if !bytes.Equal(meta.InfoBytes, infoBytes) {
		t.Fatalf("info bytes mismatch")
	}
}
