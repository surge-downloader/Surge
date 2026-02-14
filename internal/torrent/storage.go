package torrent

import (
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type FileSpan struct {
	Index  int
	Offset int64
	Len    int64
}

type FileLayout struct {
	BaseDir     string
	Info        Info
	TotalLength int64
}

func NewFileLayout(baseDir string, info Info) (*FileLayout, error) {
	if baseDir == "" {
		return nil, fmt.Errorf("base dir required")
	}
	if info.PieceLength <= 0 {
		return nil, fmt.Errorf("invalid piece length")
	}
	if info.Name == "" {
		return nil, fmt.Errorf("invalid name")
	}
	total := info.TotalLength()
	if total <= 0 {
		return nil, fmt.Errorf("invalid total length")
	}
	return &FileLayout{
		BaseDir:     baseDir,
		Info:        info,
		TotalLength: total,
	}, nil
}

func (fl *FileLayout) PieceSize(pieceIndex int64) int64 {
	totalPieces := (fl.TotalLength + fl.Info.PieceLength - 1) / fl.Info.PieceLength
	if pieceIndex < 0 || pieceIndex >= totalPieces {
		return 0
	}
	if pieceIndex == totalPieces-1 {
		last := fl.TotalLength - (pieceIndex * fl.Info.PieceLength)
		if last < 0 {
			return 0
		}
		return last
	}
	return fl.Info.PieceLength
}

func (fl *FileLayout) FilePath(i int) (string, error) {
	if fl.Info.Length > 0 {
		if i != 0 {
			return "", fmt.Errorf("single-file torrent index out of range")
		}
		return filepath.Join(fl.BaseDir, fl.Info.Name), nil
	}
	if i < 0 || i >= len(fl.Info.Files) {
		return "", fmt.Errorf("file index out of range")
	}
	parts := append([]string{fl.BaseDir, fl.Info.Name}, fl.Info.Files[i].Path...)
	return filepath.Join(parts...), nil
}

func (fl *FileLayout) ensureFiles() error {
	if fl.Info.Length > 0 {
		path, err := fl.FilePath(0)
		if err != nil {
			return err
		}
		return ensureSizedFile(path, fl.Info.Length)
	}
	for i, f := range fl.Info.Files {
		path, err := fl.FilePath(i)
		if err != nil {
			return err
		}
		if err := ensureSizedFile(path, f.Length); err != nil {
			return err
		}
	}
	return nil
}

func ensureSizedFile(path string, size int64) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Truncate(size)
}

func (fl *FileLayout) WriteAtPiece(pieceIndex int64, pieceOffset int64, data []byte) error {
	if err := fl.ensureFiles(); err != nil {
		return err
	}
	if pieceOffset < 0 {
		return fmt.Errorf("invalid piece offset")
	}
	if int64(len(data)) == 0 {
		return nil
	}
	pieceSize := fl.PieceSize(pieceIndex)
	if pieceSize == 0 {
		return fmt.Errorf("invalid piece index")
	}
	if pieceOffset+int64(len(data)) > pieceSize {
		return fmt.Errorf("write exceeds piece size")
	}

	globalOffset := pieceIndex*fl.Info.PieceLength + pieceOffset
	return fl.writeAt(globalOffset, data)
}

func (fl *FileLayout) ReadAtPiece(pieceIndex int64, pieceOffset int64, length int64) ([]byte, error) {
	if pieceOffset < 0 || length < 0 {
		return nil, fmt.Errorf("invalid piece range")
	}
	pieceSize := fl.PieceSize(pieceIndex)
	if pieceSize == 0 {
		return nil, fmt.Errorf("invalid piece index")
	}
	if pieceOffset+length > pieceSize {
		return nil, fmt.Errorf("read exceeds piece size")
	}
	if length == 0 {
		return nil, nil
	}
	buf := make([]byte, length)
	globalOffset := pieceIndex*fl.Info.PieceLength + pieceOffset
	if err := fl.readAt(globalOffset, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (fl *FileLayout) HasPiece(pieceIndex int64) bool {
	// Without resume data, we don't know which pieces are complete.
	if fl.PieceSize(pieceIndex) == 0 {
		return false
	}
	return false
}

func (fl *FileLayout) Bitfield() []byte {
	return nil
}

func (fl *FileLayout) writeAt(globalOffset int64, data []byte) error {
	if fl.Info.Length > 0 {
		path, err := fl.FilePath(0)
		if err != nil {
			return err
		}
		return writeAtFile(path, globalOffset, data)
	}
	remaining := data
	offset := globalOffset

	for i, f := range fl.Info.Files {
		if offset >= f.Length {
			offset -= f.Length
			continue
		}
		path, err := fl.FilePath(i)
		if err != nil {
			return err
		}
		// write into this file
		n := int64(len(remaining))
		if n > f.Length-offset {
			n = f.Length - offset
		}
		if err := writeAtFile(path, offset, remaining[:n]); err != nil {
			return err
		}
		remaining = remaining[n:]
		offset = 0
		if len(remaining) == 0 {
			break
		}
	}
	if len(remaining) > 0 {
		return fmt.Errorf("write exceeds torrent length")
	}
	return nil
}

func writeAtFile(path string, offset int64, data []byte) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.WriteAt(data, offset)
	return err
}

func (fl *FileLayout) ReadPiece(pieceIndex int64) ([]byte, error) {
	size := fl.PieceSize(pieceIndex)
	if size == 0 {
		return nil, fmt.Errorf("invalid piece index")
	}
	buf := make([]byte, size)
	globalOffset := pieceIndex * fl.Info.PieceLength
	if err := fl.readAt(globalOffset, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (fl *FileLayout) readAt(globalOffset int64, out []byte) error {
	if fl.Info.Length > 0 {
		path, err := fl.FilePath(0)
		if err != nil {
			return err
		}
		return readAtFile(path, globalOffset, out)
	}
	remaining := out
	offset := globalOffset
	for i, f := range fl.Info.Files {
		if offset >= f.Length {
			offset -= f.Length
			continue
		}
		path, err := fl.FilePath(i)
		if err != nil {
			return err
		}
		n := int64(len(remaining))
		if n > f.Length-offset {
			n = f.Length - offset
		}
		if err := readAtFile(path, offset, remaining[:n]); err != nil {
			return err
		}
		remaining = remaining[n:]
		offset = 0
		if len(remaining) == 0 {
			break
		}
	}
	if len(remaining) > 0 {
		return io.ErrUnexpectedEOF
	}
	return nil
}

func readAtFile(path string, offset int64, out []byte) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = f.ReadAt(out, offset)
	return err
}

func (fl *FileLayout) VerifyPiece(pieceIndex int64) (bool, error) {
	piece := fl.PieceSize(pieceIndex)
	if piece == 0 {
		return false, fmt.Errorf("invalid piece index")
	}
	hashIndex := pieceIndex * 20
	if int64(len(fl.Info.Pieces)) < hashIndex+20 {
		return false, fmt.Errorf("missing piece hash")
	}
	data, err := fl.ReadPiece(pieceIndex)
	if err != nil {
		return false, err
	}
	sum := sha1.Sum(data)
	expected := fl.Info.Pieces[hashIndex : hashIndex+20]
	return bytesEqual(sum[:], expected), nil
}

func bytesEqual(a []byte, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
