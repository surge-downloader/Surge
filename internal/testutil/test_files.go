package testutil

import (
	"crypto/rand"
	"os"
	"path/filepath"
)

// TempDir creates a temporary directory for test files and returns a cleanup function.
func TempDir(prefix string) (string, func(), error) {
	dir, err := os.MkdirTemp("", prefix+"-*")
	if err != nil {
		return "", nil, err
	}

	cleanup := func() {
		_ = os.RemoveAll(dir)
	}

	return dir, cleanup, nil
}

// CreateTestFile creates a test file with the specified size filled
// with either zeros or random data.
func CreateTestFile(dir, name string, size int64, random bool) (string, error) {
	path := filepath.Join(dir, name)

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	if random {
		// Write in chunks for large files
		chunkSize := int64(64 * 1024) // 64KB
		chunk := make([]byte, chunkSize)
		remaining := size

		for remaining > 0 {
			if remaining < chunkSize {
				chunk = make([]byte, remaining)
			}
			_, _ = rand.Read(chunk)
			n, err := f.Write(chunk)
			if err != nil {
				return "", err
			}
			remaining -= int64(n)
		}
	} else {
		// Pre-allocate with zeros (sparse file)
		if err := f.Truncate(size); err != nil {
			return "", err
		}
	}

	return path, nil
}

// CreateSurgeFile creates a .surge partial download file for resume testing.
func CreateSurgeFile(dir, name string, totalSize, downloadedSize int64) (string, error) {
	path := filepath.Join(dir, name+".surge")

	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	// Pre-allocate full size
	if err := f.Truncate(totalSize); err != nil {
		return "", err
	}

	// Fill downloaded portion with data
	if downloadedSize > 0 {
		chunk := make([]byte, min(downloadedSize, 64*1024))
		for i := range chunk {
			chunk[i] = byte(i % 256)
		}

		written := int64(0)
		for written < downloadedSize {
			remaining := downloadedSize - written
			if remaining < int64(len(chunk)) {
				chunk = chunk[:remaining]
			}
			n, err := f.WriteAt(chunk, written)
			if err != nil {
				return "", err
			}
			written += int64(n)
		}
	}

	return path, nil
}

// VerifyFileSize checks if a file has the expected size.
func VerifyFileSize(path string, expectedSize int64) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() != expectedSize {
		return &FileSizeMismatchError{
			Path:     path,
			Expected: expectedSize,
			Actual:   info.Size(),
		}
	}
	return nil
}

// FileSizeMismatchError indicates a file size doesn't match expected.
type FileSizeMismatchError struct {
	Path     string
	Expected int64
	Actual   int64
}

func (e *FileSizeMismatchError) Error() string {
	return "file size mismatch: " + e.Path
}

// CompareFiles checks if two files have identical content.
func CompareFiles(path1, path2 string) (bool, error) {
	data1, err := os.ReadFile(path1)
	if err != nil {
		return false, err
	}

	data2, err := os.ReadFile(path2)
	if err != nil {
		return false, err
	}

	if len(data1) != len(data2) {
		return false, nil
	}

	for i := range data1 {
		if data1[i] != data2[i] {
			return false, nil
		}
	}

	return true, nil
}

// ReadFileChunk reads a specific byte range from a file.
func ReadFileChunk(path string, offset, length int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	data := make([]byte, length)
	_, err = f.ReadAt(data, offset)
	if err != nil {
		return nil, err
	}

	return data, nil
}

// FileExists checks if a file exists.
func FileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
