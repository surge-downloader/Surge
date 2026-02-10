package concurrent

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/testutil"
)

func TestMirrors_HappyPath(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(20 * types.MB)

	// Server 1
	server1 := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
	)
	defer server1.Close()

	// Server 2 (Mirror)
	server2 := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
	)
	defer server2.Close()

	destPath := filepath.Join(tmpDir, "mirror_test.bin")
	state := types.NewProgressState("mirror-test", fileSize)
	runtime := &types.RuntimeConfig{
		MaxConnectionsPerHost: 4, // Enough connections to use both
	}

	downloader := NewConcurrentDownloader("mirror-test-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	mirrors := []string{server1.URL(), server2.URL()}
	// Primary URL is server1.URL()

	err := downloader.Download(ctx, server1.URL(), mirrors, mirrors, destPath, fileSize, false)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if err := testutil.VerifyFileSize(destPath, fileSize); err != nil {
		t.Error(err)
	}

	stats1 := server1.Stats()
	stats2 := server2.Stats()

	t.Logf("Server 1 requests: %d", stats1.TotalRequests)
	t.Logf("Server 2 requests: %d", stats2.TotalRequests)

	if stats1.TotalRequests == 0 || stats2.TotalRequests == 0 {
		t.Errorf("Expected requests to both servers. Server1: %d, Server2: %d", stats1.TotalRequests, stats2.TotalRequests)
	}
}

func TestMirrors_Failover(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(512 * types.KB)

	// Server 1 (Bad Server - Always returns 500)
	badHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	})
	badServer := testutil.NewHTTPServerT(t, badHandler)
	defer badServer.Close()

	// Server 2 (Good Server)
	goodServer := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithLatency(10*time.Millisecond), // Little latency to give bad server a chance to be picked first
	)
	defer goodServer.Close()

	destPath := filepath.Join(tmpDir, "failover_test.bin")
	state := types.NewProgressState("failover-test", fileSize)
	runtime := &types.RuntimeConfig{
		MaxConnectionsPerHost: 4,
		MaxTaskRetries:        5, // Need retries to switch
	}

	downloader := NewConcurrentDownloader("failover-test-id", nil, state, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Put BAD server FIRST to ensure we try it
	mirrors := []string{badServer.URL, goodServer.URL()}

	err := downloader.Download(ctx, badServer.URL, mirrors, mirrors, destPath, fileSize, false)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if err := testutil.VerifyFileSize(destPath, fileSize); err != nil {
		t.Error(err)
	}

	goodStats := goodServer.Stats()
	t.Logf("Good Server requests: %d", goodStats.TotalRequests)

	if goodStats.TotalRequests == 0 {
		t.Error("Expected good server to handle requests after failover")
	}
}
