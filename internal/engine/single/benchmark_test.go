package single

import (
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkCopyFile(b *testing.B) {
	// Setup
	tmpDir, err := os.MkdirTemp("", "surge-benchmark")
	if err != nil {
		b.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	srcPath := filepath.Join(tmpDir, "src.bin")
	dstPath := filepath.Join(tmpDir, "dst.bin")

	// Create a 100MB file
	size := int64(100 * 1024 * 1024)
	f, err := os.Create(srcPath)
	if err != nil {
		b.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		f.Close()
		b.Fatal(err)
	}
	f.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := copyFile(srcPath, dstPath)
		if err != nil {
			b.Fatal(err)
		}
	}
}
