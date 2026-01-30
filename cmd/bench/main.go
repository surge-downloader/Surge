package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	_ "net/http/pprof" // Register pprof handlers
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/surge-downloader/surge/internal/engine/concurrent"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/engine/types"
)

var (
	flagServer  = flag.Bool("server", false, "Run as benchmark server only")
	flagPort    = flag.Int("port", 0, "Port to listen on (0 for random)")
	flagSize    = flag.String("size", "2GB", "File size to serve (e.g. 500MB, 2GB)")
	flagRate    = flag.String("rate", "0", "Rate limit (e.g. 500MB/s, 100kb/s). 0 for unlimited.")
	flagPprof   = flag.Bool("pprof", false, "Enable pprof server on :6060")
	flagWriters = flag.Int("writers", 4, "Number of concurrent writer goroutines")
	flagQueue   = flag.Int("queue-size", 16, "Size of write queue")
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

func parseRate(s string) (int64, error) {
	if s == "0" || s == "" {
		return 0, nil
	}
	s = strings.ToUpper(s)
	// Remove /S or /s suffix if present
	s = strings.TrimSuffix(s, "/S")
	s = strings.TrimSuffix(s, "PS") // Mbps -> Mb

	// Handle bits vs bytes? Convention: MB = megabytes, Mb = megabits
	// For simplicity, let's assume Bytes unless 'b' is explicit, but typically benchmarks use Bytes.
	// But standard network notation is bits.
	// Let's stick to parseSize logic which is Bytes.
	// If user says "500Mbps", we might want to convert to bytes.
	// For now, let's treat everything as BYTES/s to be consistent with size flag.

	return parseSize(s)
}

func main() {
	flag.Parse()

	fileSize, err := parseSize(*flagSize)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid size: %v\n", err)
		os.Exit(1)
	}

	resRate, err := parseRate(*flagRate)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid rate: %v\n", err)
		os.Exit(1)
	}

	if *flagPprof {
		go func() {
			fmt.Println("Starting pprof server on :6060")
			if err := http.ListenAndServe("localhost:6060", nil); err != nil {
				fmt.Printf("pprof server failed: %v\n", err)
			}
		}()
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprintf("%d", fileSize))
		w.Header().Set("Accept-Ranges", "bytes") // Explicitly state support

		reader := &ZeroReader{Size: fileSize}

		// If range request, seek
		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "" {
			// Basic range handling for ZeroReader
			// http.ServeContent handles parsing Range header and calling Seek
		}

		if resRate > 0 {
			// Wrap with rate limiter
			rReader := &RateLimitedReader{
				R:     reader,
				Limit: resRate,
				Start: time.Now(),
			}
			http.ServeContent(w, r, "bench.bin", time.Now(), rReader)
		} else {
			http.ServeContent(w, r, "bench.bin", time.Now(), reader)
		}
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
		if resRate > 0 {
			fmt.Printf("Rate limit: %d bytes/s\n", resRate)
		}

		// Block forever
		if err := http.Serve(listener, handler); err != nil {
			fmt.Fprintf(os.Stderr, "Server failed: %v\n", err)
			os.Exit(1)
		}
		return
	}

	// Client Mode (Existing logic)
	ts := httptest.NewServer(handler)
	defer ts.Close()

	fmt.Printf("Benchmark Server running at %s\n", ts.URL)

	// 2. Configure Downloader
	runtime := &types.RuntimeConfig{
		MaxConnectionsPerHost: 32,
		WorkerBufferSize:      4 * 1024 * 1024, // 4MB buffer
		ConcurrentWriters:     *flagWriters,
		WriteQueueSize:        *flagQueue,
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

// RateLimitedReader wraps a ReadSeeker and limits read speed
type RateLimitedReader struct {
	R         io.ReadSeeker
	Limit     int64 // Bytes per second
	Start     time.Time
	ReadCount int64
	mu        sync.Mutex
}

func (r *RateLimitedReader) Read(p []byte) (n int, err error) {
	// Simple token bucket / target throttle implementation

	n, err = r.R.Read(p)
	if n > 0 {
		r.mu.Lock()
		r.ReadCount += int64(n)
		totalRead := r.ReadCount
		elapsed := time.Since(r.Start)
		r.mu.Unlock()

		// Expected time for totalRead bytes
		expectedDuration := time.Duration(float64(totalRead) / float64(r.Limit) * float64(time.Second))
		if expectedDuration > elapsed {
			sleepTime := expectedDuration - elapsed
			time.Sleep(sleepTime)
		}
	}
	return n, err
}

func (r *RateLimitedReader) Seek(offset int64, whence int) (int64, error) {
	return r.R.Seek(offset, whence)
}
