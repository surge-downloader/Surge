package torrent

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
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

	ensureOnce sync.Once
	ensureErr  error

	fileMu    sync.Mutex
	openFiles map[string]*os.File
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
	baseDir = filepath.Clean(baseDir)
	if abs, err := filepath.Abs(baseDir); err == nil {
		baseDir = abs
	}

	safeInfo, err := sanitizeInfo(info)
	if err != nil {
		return nil, err
	}

	total := safeInfo.TotalLength()
	if total <= 0 {
		return nil, fmt.Errorf("invalid total length")
	}
	return &FileLayout{
		BaseDir:     baseDir,
		Info:        safeInfo,
		TotalLength: total,
		openFiles:   make(map[string]*os.File),
	}, nil
}

func sanitizeInfo(info Info) (Info, error) {
	name, err := sanitizePathComponent(info.Name)
	if err != nil {
		return Info{}, fmt.Errorf("invalid torrent name: %w", err)
	}

	safe := info
	safe.Name = name

	if safe.Length > 0 {
		return safe, nil
	}

	files := make([]FileEntry, 0, len(safe.Files))
	for _, f := range safe.Files {
		if f.Length <= 0 {
			return Info{}, fmt.Errorf("invalid file length")
		}
		if len(f.Path) == 0 {
			return Info{}, fmt.Errorf("invalid file path")
		}
		parts := make([]string, 0, len(f.Path))
		for _, part := range f.Path {
			clean, err := sanitizePathComponent(part)
			if err != nil {
				return Info{}, fmt.Errorf("invalid file path component %q: %w", part, err)
			}
			parts = append(parts, clean)
		}
		files = append(files, FileEntry{
			Path:   parts,
			Length: f.Length,
		})
	}
	safe.Files = files
	return safe, nil
}

func sanitizePathComponent(part string) (string, error) {
	if part == "" {
		return "", fmt.Errorf("empty path component")
	}
	if strings.Contains(part, "\x00") {
		return "", fmt.Errorf("path contains null byte")
	}

	normalized := strings.ReplaceAll(part, "\\", "/")
	cleaned := path.Clean(normalized)

	if cleaned == "." || cleaned == "/" || cleaned == ".." {
		return "", fmt.Errorf("invalid path component")
	}
	if strings.HasPrefix(cleaned, "/") {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	if strings.Contains(cleaned, "/") {
		return "", fmt.Errorf("nested separators are not allowed in components")
	}
	return cleaned, nil
}

func safeJoin(base string, parts ...string) (string, error) {
	full := filepath.Join(append([]string{base}, parts...)...)

	absBase, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}

	rel, err := filepath.Rel(absBase, absFull)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("resolved path escapes base directory")
	}
	return absFull, nil
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
		return safeJoin(fl.BaseDir, fl.Info.Name)
	}
	if i < 0 || i >= len(fl.Info.Files) {
		return "", fmt.Errorf("file index out of range")
	}
	parts := append([]string{fl.Info.Name}, fl.Info.Files[i].Path...)
	return safeJoin(fl.BaseDir, parts...)
}

func (fl *FileLayout) ensureFiles() error {
	fl.ensureOnce.Do(func() {
		fl.ensureErr = fl.ensureFilesOnce()
	})
	return fl.ensureErr
}

func (fl *FileLayout) ensureFilesOnce() error {
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
		return fl.writeAtFile(path, globalOffset, data)
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
		if err := fl.writeAtFile(path, offset, remaining[:n]); err != nil {
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

func (fl *FileLayout) getFile(path string) (*os.File, error) {
	fl.fileMu.Lock()
	defer fl.fileMu.Unlock()

	if f, ok := fl.openFiles[path]; ok {
		return f, nil
	}

	f, err := os.OpenFile(path, os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	fl.openFiles[path] = f
	return f, nil
}

func (fl *FileLayout) writeAtFile(path string, offset int64, data []byte) error {
	f, err := fl.getFile(path)
	if err != nil {
		return err
	}
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
		return fl.readAtFile(path, globalOffset, out)
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
		if err := fl.readAtFile(path, offset, remaining[:n]); err != nil {
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

func (fl *FileLayout) readAtFile(path string, offset int64, out []byte) error {
	f, err := fl.getFile(path)
	if err != nil {
		return err
	}
	_, err = f.ReadAt(out, offset)
	return err
}

func (fl *FileLayout) Close() error {
	fl.fileMu.Lock()
	files := fl.openFiles
	fl.openFiles = make(map[string]*os.File)
	fl.fileMu.Unlock()

	var firstErr error
	for _, f := range files {
		if err := f.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (fl *FileLayout) VerifyPiece(pieceIndex int64) (bool, error) {
	data, err := fl.ReadPiece(pieceIndex)
	if err != nil {
		return false, err
	}
	return fl.VerifyPieceData(pieceIndex, data)
}

func (fl *FileLayout) VerifyPieceData(pieceIndex int64, data []byte) (bool, error) {
	pieceSize := fl.PieceSize(pieceIndex)
	if pieceSize == 0 {
		return false, fmt.Errorf("invalid piece index")
	}
	if int64(len(data)) != pieceSize {
		return false, fmt.Errorf("invalid piece data length")
	}
	hashIndex := pieceIndex * 20
	if int64(len(fl.Info.Pieces)) < hashIndex+20 {
		return false, fmt.Errorf("missing piece hash")
	}
	sum := sha1.Sum(data)
	expected := fl.Info.Pieces[hashIndex : hashIndex+20]

	// Performance: Uses heavily optimized stdlib assembly routines instead of naive Go loop
	return bytes.Equal(sum[:], expected), nil
}
