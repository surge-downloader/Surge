package torrent

import (
	"bytes"
	"crypto/sha1"
	"os"
	"path/filepath"
	"strings"
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

func TestNewFileLayoutRejectsTraversalName(t *testing.T) {
	info := Info{
		Name:        "../escape",
		PieceLength: 4,
		Length:      4,
		Pieces:      make([]byte, 20),
	}

	_, err := NewFileLayout(t.TempDir(), info)
	if err == nil {
		t.Fatalf("expected traversal name to be rejected")
	}
}

func TestNewFileLayoutRejectsTraversalFilePath(t *testing.T) {
	info := Info{
		Name:        "dir",
		PieceLength: 4,
		Pieces:      make([]byte, 20),
		Files: []FileEntry{
			{
				Path:   []string{"..", "escape.bin"},
				Length: 4,
			},
		},
	}

	_, err := NewFileLayout(t.TempDir(), info)
	if err == nil {
		t.Fatalf("expected traversal file path to be rejected")
	}
}

func TestFilePathAlwaysInsideBaseDir(t *testing.T) {
	dir := t.TempDir()
	info := Info{
		Name:        "safe",
		PieceLength: 4,
		Pieces:      make([]byte, 20),
		Files: []FileEntry{
			{
				Path:   []string{"nested", "a.bin"},
				Length: 4,
			},
		},
	}

	fl, err := NewFileLayout(dir, info)
	if err != nil {
		t.Fatalf("layout err: %v", err)
	}
	got, err := fl.FilePath(0)
	if err != nil {
		t.Fatalf("filepath err: %v", err)
	}

	rel, err := filepath.Rel(dir, got)
	if err != nil {
		t.Fatalf("rel err: %v", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		t.Fatalf("path escaped base dir: %s", got)
	}
}

func TestVerifyPieceData(t *testing.T) {
	data := []byte("ABCD")
	hash := sha1.Sum(data)

	info := Info{
		Name:        "piece.bin",
		PieceLength: int64(len(data)),
		Length:      int64(len(data)),
		Pieces:      hash[:],
	}

	fl, err := NewFileLayout(t.TempDir(), info)
	if err != nil {
		t.Fatalf("layout err: %v", err)
	}

	ok, err := fl.VerifyPieceData(0, data)
	if err != nil || !ok {
		t.Fatalf("expected piece data to verify, ok=%v err=%v", ok, err)
	}

	ok, err = fl.VerifyPieceData(0, []byte("WXYZ"))
	if err != nil {
		t.Fatalf("unexpected verify error: %v", err)
	}
	if ok {
		t.Fatalf("expected invalid piece data to fail verification")
	}
}
