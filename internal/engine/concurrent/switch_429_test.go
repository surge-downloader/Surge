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

func TestConcurrentDownloader_SwitchOn429(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(256 * types.KB)

	// Server 1: Always returns 429
	server1 := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithHandler(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}),
	)
	defer server1.Close()

	// Server 2: Works normally
	server2 := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
	)
	defer server2.Close()

	destPath := filepath.Join(tmpDir, "switch429_test.bin")
	state := types.NewProgressState("switch429-test", fileSize)

	// Use large retry delay to prove we skipped it
	// If we didn't skip it, the test would take > 1 second (since retry adds backoff)
	// Base delay is 200ms. 1st retry = 400ms.
	// We want to be sure.
	runtime := &types.RuntimeConfig{
		MaxConnectionsPerHost: 1, // Single worker to trace behavior easily
		MaxTaskRetries:        5,
		MinChunkSize:          64 * types.KB,
	}

	downloader := NewConcurrentDownloader("switch429-id", nil, state, runtime)

	// List mirrors: Server 1 (Bad), Server 2 (Good)
	mirrors := []string{server1.URL(), server2.URL()}

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Pass server1 as primary, but provide both in mirrors list
	err := downloader.Download(ctx, server1.URL(), mirrors, mirrors, destPath, fileSize, false)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if err := testutil.VerifyFileSize(destPath, fileSize); err != nil {
		t.Error(err)
	}

	// Verification:
	// If backoff was APPLIED, we would have slept (attempt 1 = 400ms).
	// But since we have 2 mirrors, we SKIP backoff.
	// So it should be very fast (< 200ms).
	if elapsed > 200*time.Millisecond {
		t.Errorf("Download took %v, indicating backoff was applied via sleep (expected skip)", elapsed)
	}
}

func TestConcurrentDownloader_BackoffOnSingleMirror(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(1 * types.MB) // Use enough size so it doesn't just finish instantly on 1st byte

	// Server: Returns 429 once, then succeeds
	// This forces a retry.
	server := testutil.NewMockServerT(t,
		testutil.WithFileSize(fileSize),
		testutil.WithFileSize(fileSize),
		testutil.WithRangeSupport(true),
		testutil.WithFailOnNthRequest(1), // Fail 1st request (Worker's 1st try)
	)
	defer server.Close()

	destPath := filepath.Join(tmpDir, "backoff_test.bin")
	state := types.NewProgressState("backoff-test", fileSize)

	// Runtime with 1 connection and retries
	// RetryBaseDelay is 200ms by default in types
	runtime := &types.RuntimeConfig{
		MaxConnectionsPerHost: 1,
		MaxTaskRetries:        5,
		MinChunkSize:          64 * types.KB,
	}

	downloader := NewConcurrentDownloader("backoff-id", nil, state, runtime)

	// Single mirror
	mirrors := []string{} // No other mirrors

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Only 1 URL (server.URL) is valid
	err := downloader.Download(ctx, server.URL(), mirrors, nil, destPath, fileSize, false)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// Verification:
	// We experienced 1 failure (429).
	// Attempt 1 backoff = 2^1 * BaseDelay (200ms) = 400ms.
	// So it should be > 200ms.
	if elapsed < 200*time.Millisecond {
		t.Errorf("Download took %v, but expected backoff wait (should be > 200ms)", elapsed)
	}
}
