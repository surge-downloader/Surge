package concurrent

import (
	"context"
	"net/http"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/testutil"
)

// TestConcurrentDownloader_CustomHeaders verifies that custom headers from browser
// extension are correctly forwarded to the download server.
func TestConcurrentDownloader_CustomHeaders(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(64 * types.KB)

	// Track received headers
	var mu sync.Mutex
	var receivedCookie string
	var receivedAuth string
	var receivedReferer string
	var receivedUserAgent string
	requestCount := 0

	// Custom handler to capture headers
	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		// Capture headers from first request (all requests should have same custom headers)
		if requestCount == 1 {
			receivedCookie = r.Header.Get("Cookie")
			receivedAuth = r.Header.Get("Authorization")
			receivedReferer = r.Header.Get("Referer")
			receivedUserAgent = r.Header.Get("User-Agent")
		}
		mu.Unlock()

		// Serve the file content with range support
		w.Header().Set("Content-Length", "65536")
		w.Header().Set("Accept-Ranges", "bytes")

		// Handle range requests
		rangeHeader := r.Header.Get("Range")
		if rangeHeader != "" {
			w.Header().Set("Content-Range", "bytes 0-65535/65536")
			w.WriteHeader(http.StatusPartialContent)
		}

		// Write file content (zeros)
		data := make([]byte, fileSize)
		_, _ = w.Write(data)
	}))
	defer server.Close()

	destPath := filepath.Join(tmpDir, "headers_test.bin")
	progState := types.NewProgressState("headers-test", fileSize)
	runtime := &types.RuntimeConfig{MaxConnectionsPerHost: 2}

	downloader := NewConcurrentDownloader("headers-test", nil, progState, runtime)

	// Set custom headers from "browser extension"
	downloader.Headers = map[string]string{
		"Cookie":        "session_id=abc123; user_token=xyz789",
		"Authorization": "Bearer test-jwt-token",
		"Referer":       "https://example.com/downloads",
		"User-Agent":    "CustomBrowser/1.0",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := downloader.Download(ctx, server.URL, nil, nil, destPath, fileSize, false)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// Verify headers were received
	mu.Lock()
	defer mu.Unlock()

	if receivedCookie != "session_id=abc123; user_token=xyz789" {
		t.Errorf("Cookie header not forwarded correctly. Got: %q", receivedCookie)
	}
	if receivedAuth != "Bearer test-jwt-token" {
		t.Errorf("Authorization header not forwarded correctly. Got: %q", receivedAuth)
	}
	if receivedReferer != "https://example.com/downloads" {
		t.Errorf("Referer header not forwarded correctly. Got: %q", receivedReferer)
	}
	if receivedUserAgent != "CustomBrowser/1.0" {
		t.Errorf("User-Agent header should use custom value. Got: %q", receivedUserAgent)
	}

	// Verify file was created
	if err := testutil.VerifyFileSize(destPath, fileSize); err != nil {
		t.Error(err)
	}
}

// TestConcurrentDownloader_DefaultUserAgent verifies that the default User-Agent
// is used when custom headers don't include one.
func TestConcurrentDownloader_DefaultUserAgent(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(32 * types.KB)

	var mu sync.Mutex
	var receivedUserAgent string

	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if receivedUserAgent == "" {
			receivedUserAgent = r.Header.Get("User-Agent")
		}
		mu.Unlock()

		w.Header().Set("Content-Length", "32768")
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusPartialContent)
		}
		data := make([]byte, fileSize)
		_, _ = w.Write(data)
	}))
	defer server.Close()

	destPath := filepath.Join(tmpDir, "default_ua_test.bin")
	progState := types.NewProgressState("ua-test", fileSize)
	runtime := &types.RuntimeConfig{
		MaxConnectionsPerHost: 1,
		UserAgent:             "SurgeDownloader/1.0",
	}

	downloader := NewConcurrentDownloader("ua-test", nil, progState, runtime)

	// Set custom headers WITHOUT User-Agent
	downloader.Headers = map[string]string{
		"Cookie": "session=test",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := downloader.Download(ctx, server.URL, nil, nil, destPath, fileSize, false)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Should use default User-Agent from runtime config when not provided in headers
	if receivedUserAgent != "SurgeDownloader/1.0" {
		t.Errorf("Expected default User-Agent, got: %q", receivedUserAgent)
	}
}

// TestConcurrentDownloader_RangeHeaderNotOverridden verifies that custom Range
// headers from browser are ignored (we must use our own for parallel downloads).
func TestConcurrentDownloader_RangeHeaderNotOverridden(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(32 * types.KB)

	var mu sync.Mutex
	var receivedRange string

	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if receivedRange == "" {
			receivedRange = r.Header.Get("Range")
		}
		mu.Unlock()

		w.Header().Set("Content-Length", "32768")
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusPartialContent)
		}
		data := make([]byte, fileSize)
		_, _ = w.Write(data)
	}))
	defer server.Close()

	destPath := filepath.Join(tmpDir, "range_test.bin")
	progState := types.NewProgressState("range-test", fileSize)
	runtime := &types.RuntimeConfig{MaxConnectionsPerHost: 1}

	downloader := NewConcurrentDownloader("range-test", nil, progState, runtime)

	// Set custom Range header (should be ignored/overridden)
	downloader.Headers = map[string]string{
		"Range": "bytes=0-100", // This should NOT be used
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := downloader.Download(ctx, server.URL, nil, nil, destPath, fileSize, false)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	// Range should be set by our code, not the custom header
	if receivedRange == "bytes=0-100" {
		t.Errorf("Custom Range header should be ignored, but got: %q", receivedRange)
	}
	if receivedRange == "" {
		t.Errorf("Range header should have been set by downloader")
	}
}

// TestConcurrentDownloader_HeadersForwardedOnRedirect verifies that custom headers
// are preserved when the server redirects to a different domain (e.g., load balancer).
// This is the fix for authenticated downloads from sites like easynews.com that redirect
// from members.easynews.com to iad-dl-08.easynews.com.
func TestConcurrentDownloader_HeadersForwardedOnRedirect(t *testing.T) {
	tmpDir, cleanup := initTestState(t)
	defer cleanup()

	fileSize := int64(32 * types.KB)

	// Track received headers on the final server
	var mu sync.Mutex
	var receivedCookie string
	var receivedAuth string
	redirectCount := 0

	// Final server (simulates the actual file server after redirect)
	finalServer := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedCookie = r.Header.Get("Cookie")
		receivedAuth = r.Header.Get("Authorization")
		mu.Unlock()

		w.Header().Set("Content-Length", "32768")
		w.Header().Set("Accept-Ranges", "bytes")
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusPartialContent)
		}
		data := make([]byte, fileSize)
		_, _ = w.Write(data)
	}))
	defer finalServer.Close()

	// Redirect server (simulates members.easynews.com redirecting to iad-dl-08.easynews.com)
	redirectServer := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		redirectCount++
		mu.Unlock()

		// Redirect to the final server
		http.Redirect(w, r, finalServer.URL+"/file.bin", http.StatusFound)
	}))
	defer redirectServer.Close()

	destPath := filepath.Join(tmpDir, "redirect_headers_test.bin")
	progState := types.NewProgressState("redirect-headers-test", fileSize)
	runtime := &types.RuntimeConfig{MaxConnectionsPerHost: 1}

	downloader := NewConcurrentDownloader("redirect-headers-test", nil, progState, runtime)

	// Set headers that should be forwarded through the redirect
	downloader.Headers = map[string]string{
		"Cookie":        "auth_session=secret123",
		"Authorization": "Basic dXNlcjpwYXNz", // base64 of user:pass
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err := downloader.Download(ctx, redirectServer.URL, nil, nil, destPath, fileSize, false)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	// Verify headers were forwarded to the final server after redirect
	mu.Lock()
	defer mu.Unlock()

	if redirectCount == 0 {
		t.Error("Expected at least one redirect")
	}

	if receivedCookie != "auth_session=secret123" {
		t.Errorf("Cookie header not forwarded after redirect. Got: %q", receivedCookie)
	}
	if receivedAuth != "Basic dXNlcjpwYXNz" {
		t.Errorf("Authorization header not forwarded after redirect. Got: %q", receivedAuth)
	}

	// Verify file was created
	if err := testutil.VerifyFileSize(destPath, fileSize); err != nil {
		t.Error(err)
	}
}
