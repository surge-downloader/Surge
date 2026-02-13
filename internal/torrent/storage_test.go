package torrent

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestFileLayout_SingleFileWriteRead(t *testing.T) {
	info := Info{
		Name:        "file.bin",
		PieceLength: 4,
		Length:      10,
		Pieces:      make([]byte, 20*3),
	}
	dir := t.TempDir()
	fl, err := NewFileLayout(dir, info)
	if err != nil {
		t.Fatalf("layout err: %v", err)
	}

	data := []byte("ABCD")
	if err := fl.WriteAtPiece(1, 0, data); err != nil {
		t.Fatalf("write err: %v", err)
	}
	read, err := fl.ReadPiece(1)
	if err != nil {
		t.Fatalf("read err: %v", err)
	}
	if !bytes.Equal(read, data) {
		t.Fatalf("mismatch")
	}

	path := filepath.Join(dir, "file.bin")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created: %v", err)
	}
}

func TestFileLayout_MultiFileWriteRead(t *testing.T) {
	info := Info{
		Name:        "dir",
		PieceLength: 4,
		Pieces:      make([]byte, 20*3),
		Files: []FileEntry{
			{Path: []string{"a.bin"}, Length: 3},
			{Path: []string{"b.bin"}, Length: 5},
			{Path: []string{"c.bin"}, Length: 2},
		},
	}
	dir := t.TempDir()
	fl, err := NewFileLayout(dir, info)
	if err != nil {
		t.Fatalf("layout err: %v", err)
	}

	// piece 0 covers a(3) + b(1)
	if err := fl.WriteAtPiece(0, 0, []byte("ABCD")); err != nil {
		t.Fatalf("write err: %v", err)
	}
	// piece 1 covers b(4)
	if err := fl.WriteAtPiece(1, 0, []byte("EFGH")); err != nil {
		t.Fatalf("write err: %v", err)
	}
	// piece 2 covers c(2)
	if err := fl.WriteAtPiece(2, 0, []byte("IJ")); err != nil {
		t.Fatalf("write err: %v", err)
	}

	got, err := fl.ReadPiece(0)
	if err != nil {
		t.Fatalf("read err: %v", err)
	}
	if !bytes.Equal(got, []byte("ABCD")) {
		t.Fatalf("piece0 mismatch")
	}
}
