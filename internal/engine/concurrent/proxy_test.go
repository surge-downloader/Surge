package concurrent

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/testutil"
)

func TestConcurrentDownloader_ProxySupport(t *testing.T) {
	// 1. Setup Mock Target Server
	targetServer := testutil.NewMockServerT(t,
		testutil.WithFileSize(1024),
		testutil.WithRangeSupport(true),
	)
	defer targetServer.Close()

	// 2. Setup Mock Proxy Server
	proxyHit := false
	proxyServer := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHit = true
		// Forward request to target
		// Note: A real proxy would handle CONNECT or absolute URLs.
		// For this test, the client will send an absolute URL to the proxy.

		// Create request to target
		req, err := http.NewRequest(r.Method, r.RequestURI, r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		// Copy headers
		for k, v := range r.Header {
			req.Header[k] = v
		}

		// Execute
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()

		// Copy response
		for k, v := range resp.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	defer proxyServer.Close()

	// 3. Configure Downloader with Proxy
	runtime := &types.RuntimeConfig{
		MaxConnectionsPerHost: 1,
		MinChunkSize:          1024,
		WorkerBufferSize:      1024,
		ProxyURL:              proxyServer.URL,
	}

	// Create temp dir for output
	tmpDir, cleanup, err := testutil.TempDir("proxy-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer cleanup()

	destPath := tmpDir + "/proxy-download.bin"

	downloader := NewConcurrentDownloader("test-id", nil, nil, runtime)

	// 4. Execute Download
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = downloader.Download(ctx, targetServer.URL(), nil, nil, destPath, 1024, false)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// 5. Verify Proxy was used
	if !proxyHit {
		t.Error("Proxy was NOT used during download")
	}

	// 6. Verify File Content
	if err := testutil.VerifyFileSize(destPath, 1024); err != nil {
		t.Errorf("File verification failed: %v", err)
	}
}

func TestConcurrentDownloader_InvalidProxy(t *testing.T) {
	// Should fallback to direct connection or fail gracefully?
	// Implementation currently falls back to environment/direct on invalid URL parse,
	// but let's test that it doesn't panic.

	targetServer := testutil.NewMockServerT(t, testutil.WithFileSize(1024))
	defer targetServer.Close()

	runtime := &types.RuntimeConfig{
		MaxConnectionsPerHost: 1,
		ProxyURL:              "://invalid-url",
	}

	tmpDir, cleanup, _ := testutil.TempDir("proxy-fail-test")
	defer cleanup()
	destPath := tmpDir + "/output.bin"

	downloader := NewConcurrentDownloader("test-id-2", nil, nil, runtime)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// This should hopefully succeed by ignoring the invalid proxy, or fail with a network error
	// The key is that it shouldn't panic.
	// Since we log error and fallback, it should succeed if direct connection works.
	err := downloader.Download(ctx, targetServer.URL(), nil, nil, destPath, 1024, false)
	if err != nil {
		t.Logf("Download failed as expected or unexpected: %v", err)
	}
}
