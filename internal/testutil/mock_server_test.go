package testutil

import (
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestMockServer_BasicDownload(t *testing.T) {
	server := NewMockServerT(t,
		WithFileSize(1024*1024), // 1MB
		WithRangeSupport(true),
	)
	defer server.Close()

	// Full download
	resp, err := http.Get(server.URL())
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read: %v", err)
	}

	if int64(len(data)) != 1024*1024 {
		t.Errorf("Expected 1MB, got %d bytes", len(data))
	}

	stats := server.Stats()
	if stats.TotalRequests != 1 {
		t.Errorf("Expected 1 request, got %d", stats.TotalRequests)
	}
	if stats.FullRequests != 1 {
		t.Errorf("Expected 1 full request, got %d", stats.FullRequests)
	}
}

func TestMockServer_RangeRequest(t *testing.T) {
	server := NewMockServerT(t,
		WithFileSize(1024*1024), // 1MB
		WithRangeSupport(true),
	)
	defer server.Close()

	// Range request for first 1024 bytes
	client := &http.Client{}
	req, _ := http.NewRequest("GET", server.URL(), nil)
	req.Header.Set("Range", "bytes=0-1023")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusPartialContent {
		t.Errorf("Expected 206, got %d", resp.StatusCode)
	}

	data, _ := io.ReadAll(resp.Body)
	if len(data) != 1024 {
		t.Errorf("Expected 1024 bytes, got %d", len(data))
	}

	stats := server.Stats()
	if stats.RangeRequests != 1 {
		t.Errorf("Expected 1 range request, got %d", stats.RangeRequests)
	}
}

func TestMockServer_MultipleRangeRequests(t *testing.T) {
	fileSize := int64(1024 * 1024) // 1MB
	server := NewMockServerT(t,
		WithFileSize(fileSize),
		WithRangeSupport(true),
	)
	defer server.Close()

	client := &http.Client{}

	// Simulate chunked download
	chunkSize := int64(256 * 1024) // 256KB chunks
	var totalRead int64

	for offset := int64(0); offset < fileSize; offset += chunkSize {
		end := offset + chunkSize - 1
		if end >= fileSize {
			end = fileSize - 1
		}

		req, _ := http.NewRequest("GET", server.URL(), nil)
		req.Header.Set("Range", "bytes="+formatRange(offset, end))

		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Chunk %d failed: %v", offset/chunkSize, err)
		}

		data, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()

		expectedLen := end - offset + 1
		if int64(len(data)) != expectedLen {
			t.Errorf("Chunk at %d: expected %d bytes, got %d", offset, expectedLen, len(data))
		}
		totalRead += int64(len(data))
	}

	if totalRead != fileSize {
		t.Errorf("Total read: expected %d, got %d", fileSize, totalRead)
	}

	stats := server.Stats()
	expectedRequests := (fileSize + chunkSize - 1) / chunkSize
	if stats.RangeRequests != expectedRequests {
		t.Errorf("Expected %d range requests, got %d", expectedRequests, stats.RangeRequests)
	}
}

func TestMockServer_HeadRequest(t *testing.T) {
	server := NewMockServerT(t,
		WithFileSize(5*1024*1024), // 5MB
		WithRangeSupport(true),
		WithFilename("testfile.zip"),
	)
	defer server.Close()

	client := &http.Client{}
	req, _ := http.NewRequest("HEAD", server.URL(), nil)

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("HEAD request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200, got %d", resp.StatusCode)
	}

	if resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Error("Expected Accept-Ranges: bytes")
	}

	contentLength := resp.Header.Get("Content-Length")
	if contentLength != "5242880" {
		t.Errorf("Expected Content-Length 5242880, got %s", contentLength)
	}
}

func TestMockServer_NoRangeSupport(t *testing.T) {
	server := NewMockServerT(t,
		WithFileSize(1024),
		WithRangeSupport(false),
	)
	defer server.Close()

	client := &http.Client{}
	req, _ := http.NewRequest("GET", server.URL(), nil)
	req.Header.Set("Range", "bytes=0-511")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Should return full file, not partial
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected 200 (no range support), got %d", resp.StatusCode)
	}

	// Should get full file
	data, _ := io.ReadAll(resp.Body)
	if len(data) != 1024 {
		t.Errorf("Expected full 1024 bytes, got %d", len(data))
	}
}

func TestMockServer_FailOnNthRequest(t *testing.T) {
	server := NewMockServerT(t,
		WithFileSize(1024),
		WithFailOnNthRequest(2),
	)
	defer server.Close()

	// First request should succeed
	resp1, _ := http.Get(server.URL())
	if resp1.StatusCode != http.StatusOK {
		t.Errorf("First request should succeed, got %d", resp1.StatusCode)
	}
	_ = resp1.Body.Close()

	// Second request should fail
	resp2, _ := http.Get(server.URL())
	if resp2.StatusCode != http.StatusInternalServerError {
		t.Errorf("Second request should fail, got %d", resp2.StatusCode)
	}
	_ = resp2.Body.Close()

	// Third request should succeed
	resp3, _ := http.Get(server.URL())
	if resp3.StatusCode != http.StatusOK {
		t.Errorf("Third request should succeed, got %d", resp3.StatusCode)
	}
	_ = resp3.Body.Close()

	stats := server.Stats()
	if stats.FailedRequests != 1 {
		t.Errorf("Expected 1 failed request, got %d", stats.FailedRequests)
	}
}

func TestMockServer_Latency(t *testing.T) {
	latency := 100 * time.Millisecond
	server := NewMockServerT(t,
		WithFileSize(1024),
		WithLatency(latency),
	)
	defer server.Close()

	start := time.Now()
	resp, _ := http.Get(server.URL())
	_ = resp.Body.Close()
	elapsed := time.Since(start)

	if elapsed < latency {
		t.Errorf("Request should have at least %v latency, took %v", latency, elapsed)
	}
}

func TestMockServer_Reset(t *testing.T) {
	server := NewMockServerT(t, WithFileSize(1024))
	defer server.Close()

	// Make a request
	resp, _ := http.Get(server.URL())
	_ = resp.Body.Close()

	if server.Stats().TotalRequests != 1 {
		t.Error("Should have 1 request")
	}

	// Reset
	server.Reset()

	if server.Stats().TotalRequests != 0 {
		t.Error("Should have 0 requests after reset")
	}
}

func TestStreamingMockServer_LargeFile(t *testing.T) {
	// 100MB virtual file (doesn't actually allocate 100MB)
	fileSize := int64(100 * 1024 * 1024)
	server := NewStreamingMockServerT(t, fileSize, WithRangeSupport(true))
	defer server.Close()

	// Just request a small range to verify it works
	client := &http.Client{}
	req, _ := http.NewRequest("GET", server.URL(), nil)
	req.Header.Set("Range", "bytes=0-1023")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusPartialContent {
		t.Errorf("Expected 206, got %d", resp.StatusCode)
	}

	data, _ := io.ReadAll(resp.Body)
	if len(data) != 1024 {
		t.Errorf("Expected 1024 bytes, got %d", len(data))
	}
}

func formatRange(start, end int64) string {
	return strconv.FormatInt(start, 10) + "-" + strconv.FormatInt(end, 10)
}

func TestTempDir(t *testing.T) {
	dir, cleanup, err := TempDir("surge-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer cleanup()

	if dir == "" {
		t.Error("TempDir returned empty string")
	}

	if !FileExists(dir) {
		t.Error("TempDir directory doesn't exist")
	}

	// After cleanup, dir should not exist
	cleanup()
	if FileExists(dir) {
		t.Error("TempDir should be removed after cleanup")
	}
}

func TestCreateTestFile(t *testing.T) {
	dir, cleanup, err := TempDir("surge-test")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	path, err := CreateTestFile(dir, "test.bin", 1024, false)
	if err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	if err := VerifyFileSize(path, 1024); err != nil {
		t.Error(err)
	}
}

func TestVerifyFileSize(t *testing.T) {
	dir, cleanup, _ := TempDir("surge-test")
	defer cleanup()

	path, _ := CreateTestFile(dir, "test.bin", 2048, false)

	if err := VerifyFileSize(path, 2048); err != nil {
		t.Errorf("Should match: %v", err)
	}

	if err := VerifyFileSize(path, 1024); err == nil {
		t.Error("Should fail for wrong size")
	}
}
