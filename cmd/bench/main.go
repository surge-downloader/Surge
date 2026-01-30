package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/surge-downloader/surge/internal/engine/concurrent"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/engine/types"
)

var (
	flagServer = flag.Bool("server", false, "Run as benchmark server only")
	flagPort   = flag.Int("port", 0, "Port to listen on (0 for random)")
	flagSize   = flag.String("size", "2GB", "File size to serve (e.g. 500MB, 2GB)")
)

func parseSize(s string) (int64, error) {
	s = strings.ToUpper(s)
	multiplier := int64(1)
	if strings.HasSuffix(s, "GB") {
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "GB")
	} else if strings.HasSuffix(s, "MB") {
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(s, "MB")
	} else if strings.HasSuffix(s, "KB") {
		multiplier = 1024
		s = strings.TrimSuffix(s, "KB")
	}

	val, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	return val * multiplier, nil
}

func main() {
	flag.Parse()

	fileSize, err := parseSize(*flagSize)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid size: %v\n", err)
		os.Exit(1)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", fileSize))
		http.ServeContent(w, r, "bench.bin", time.Now(), &ZeroReader{Size: fileSize})
	})

	if *flagServer {
		var listener net.Listener
		var err error
		addr := fmt.Sprintf("127.0.0.1:%d", *flagPort)

		listener, err = net.Listen("tcp", addr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to listen: %v\n", err)
			os.Exit(1)
		}

		url := fmt.Sprintf("http://%s/bench.bin", listener.Addr().String())
		fmt.Printf("Server listening on %s\n", url)

		// Block forever
		if err := http.Serve(listener, handler); err != nil {
			fmt.Fprintf(os.Stderr, "Server failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Client Mode (Existing logic)
	// Start an internal server anyway for the self-test if we are not connecting elsewhere
	// But the purpose here is to test the download engine.
	// We'll keep the existing logic that spins up a test server automatically
	// because it's convenient for `go run ./cmd/bench`.

	ts := httptest.NewServer(handler)
	defer ts.Close()

	fmt.Printf("Benchmark Server running at %s\n", ts.URL)

	// 2. Configure Downloader
	runtime := &types.RuntimeConfig{
		MaxConnectionsPerHost: 32,
		WorkerBufferSize:      4 * 1024 * 1024, // 4MB buffer
	}

	progressCh := make(chan any, 100)
	stateDesc := types.NewProgressState("bench-1", fileSize)

	// 3. Output to /dev/shm to avoid disk IO bottleneck
	// If /dev/shm not available, use temp dir
	destDir := "/dev/shm"
	if _, err := os.Stat(destDir); err != nil {
		destDir = os.TempDir()
	}

	// Configure State DB
	dbPath := filepath.Join(destDir, "surge-bench.db")
	state.Configure(dbPath)

	destPath := filepath.Join(destDir, "surge-bench.bin")

	// Cleanup previous
	os.Remove(destPath)
	os.Remove(destPath + ".surge") // Incomplete suffix
	os.Remove(dbPath)              // Cleanup DB

	downloader := concurrent.NewConcurrentDownloader("bench-1", progressCh, stateDesc, runtime)

	fmt.Printf("Downloading %d MB to %s...\n", fileSize/1024/1024, destPath)

	start := time.Now()
	// Use the URL from our internal test server
	err = downloader.Download(context.Background(), ts.URL, destPath, fileSize, false)
	duration := time.Since(start)

	if err != nil {
		fmt.Fprintf(os.Stderr, "Download failed: %v\n", err)
		os.Exit(1)
	}

	// 4. Report
	mbps := float64(fileSize) / 1024 / 1024 / duration.Seconds()
	fmt.Printf("Download completed in %v\n", duration)
	fmt.Printf("Average Speed: %.2f MB/s\n", mbps)

	// Cleanup
	os.Remove(destPath)
	os.Remove(dbPath)
}

// ZeroReader implements io.ReadSeeker for zeros
type ZeroReader struct {
	Size int64
	pos  int64
}

func (z *ZeroReader) Read(p []byte) (n int, err error) {
	if z.pos >= z.Size {
		return 0, io.EOF
	}
	remaining := z.Size - z.pos
	if int64(len(p)) > remaining {
		n = int(remaining)
	} else {
		n = len(p)
	}
	z.pos += int64(n)
	return n, nil
}

func (z *ZeroReader) Seek(offset int64, whence int) (int64, error) {
	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = z.pos + offset
	case io.SeekEnd:
		newPos = z.Size + offset
	}
	if newPos < 0 {
		return 0, fmt.Errorf("invalid seek")
	}
	z.pos = newPos
	return newPos, nil
}
