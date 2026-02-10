// Package testutil provides testing utilities for the Surge download manager.
package testutil

import (
	"crypto/rand"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// MockServer is a configurable HTTP test server for download testing.
type MockServer struct {
	Server *httptest.Server

	// Configuration
	FileSize          int64         // Size of the served file
	SupportsRanges    bool          // Whether to support HTTP Range requests
	ContentType       string        // Content-Type header value
	Filename          string        // Filename in Content-Disposition header
	RandomData        bool          // If true, serve random data; otherwise serve zeros
	Latency           time.Duration // Artificial latency per request
	ByteLatency       time.Duration // Latency per byte (simulates slow connection)
	FailAfterBytes    int64         // Fail connection after this many bytes (0 = no fail)
	FailOnNthRequest  int           // Fail on Nth request (0 = don't fail)
	MaxConcurrentReqs int           // Max concurrent requests (0 = unlimited)

	// Tracking
	RequestCount   atomic.Int64
	BytesServed    atomic.Int64
	ActiveRequests atomic.Int64
	RangeRequests  atomic.Int64
	FullRequests   atomic.Int64
	FailedRequests atomic.Int64
	requestCountMu sync.Mutex
	internalReqNum int

	// Internal
	data          []byte
	CustomHandler http.HandlerFunc
}

// MockServerOption is a function that configures a MockServer.
type MockServerOption func(*MockServer)

// WithHandler sets a custom request handler.
func WithHandler(h http.HandlerFunc) MockServerOption {
	return func(m *MockServer) {
		m.CustomHandler = h
	}
}

// WithFileSize sets the file size to serve.
func WithFileSize(size int64) MockServerOption {
	return func(m *MockServer) {
		m.FileSize = size
	}
}

// WithRangeSupport enables or disables Range request support.
func WithRangeSupport(enabled bool) MockServerOption {
	return func(m *MockServer) {
		m.SupportsRanges = enabled
	}
}

// WithContentType sets the Content-Type header.
func WithContentType(ct string) MockServerOption {
	return func(m *MockServer) {
		m.ContentType = ct
	}
}

// WithFilename sets the filename in Content-Disposition header.
func WithFilename(name string) MockServerOption {
	return func(m *MockServer) {
		m.Filename = name
	}
}

// WithRandomData enables serving random bytes instead of zeros.
func WithRandomData(random bool) MockServerOption {
	return func(m *MockServer) {
		m.RandomData = random
	}
}

// WithLatency adds artificial latency per request.
func WithLatency(d time.Duration) MockServerOption {
	return func(m *MockServer) {
		m.Latency = d
	}
}

// WithByteLatency adds artificial latency per byte served.
func WithByteLatency(d time.Duration) MockServerOption {
	return func(m *MockServer) {
		m.ByteLatency = d
	}
}

// WithFailAfterBytes causes the connection to fail after serving N bytes.
func WithFailAfterBytes(n int64) MockServerOption {
	return func(m *MockServer) {
		m.FailAfterBytes = n
	}
}

// WithFailOnNthRequest causes the Nth request to fail.
func WithFailOnNthRequest(n int) MockServerOption {
	return func(m *MockServer) {
		m.FailOnNthRequest = n
	}
}

// WithMaxConcurrentRequests limits concurrent requests.
func WithMaxConcurrentRequests(n int) MockServerOption {
	return func(m *MockServer) {
		m.MaxConcurrentReqs = n
	}
}

// NewMockServer creates a new mock HTTP server with the given options.
func NewMockServer(opts ...MockServerOption) *MockServer {
	m := &MockServer{
		FileSize:       1024 * 1024, // 1MB default
		SupportsRanges: true,
		ContentType:    "application/octet-stream",
		Filename:       "testfile.bin",
		RandomData:     false,
	}

	for _, opt := range opts {
		opt(m)
	}

	// Pre-generate data
	m.data = make([]byte, m.FileSize)
	if m.RandomData {
		_, _ = rand.Read(m.data)
	}

	m.Server = NewHTTPServer(http.HandlerFunc(m.handleRequest))
	return m
}

// NewMockServerT creates a new mock HTTP server and skips the test if binding fails.
func NewMockServerT(t *testing.T, opts ...MockServerOption) *MockServer {
	t.Helper()
	m := &MockServer{
		FileSize:       1024 * 1024,
		SupportsRanges: true,
		ContentType:    "application/octet-stream",
		Filename:       "testfile.bin",
		RandomData:     false,
	}

	for _, opt := range opts {
		opt(m)
	}

	m.data = make([]byte, m.FileSize)
	if m.RandomData {
		_, _ = rand.Read(m.data)
	}

	m.Server = NewHTTPServerT(t, http.HandlerFunc(m.handleRequest))
	return m
}

// URL returns the server's URL.
func (m *MockServer) URL() string {
	return m.Server.URL
}

// Close shuts down the mock server.
func (m *MockServer) Close() {
	if m.Server != nil {
		m.Server.Close()
	}
}

// Reset clears all tracking counters.
func (m *MockServer) Reset() {
	m.RequestCount.Store(0)
	m.BytesServed.Store(0)
	m.ActiveRequests.Store(0)
	m.RangeRequests.Store(0)
	m.FullRequests.Store(0)
	m.FailedRequests.Store(0)
	m.requestCountMu.Lock()
	m.internalReqNum = 0
	m.requestCountMu.Unlock()
}

// Stats returns a summary of server statistics.
func (m *MockServer) Stats() MockServerStats {
	return MockServerStats{
		TotalRequests:  m.RequestCount.Load(),
		BytesServed:    m.BytesServed.Load(),
		RangeRequests:  m.RangeRequests.Load(),
		FullRequests:   m.FullRequests.Load(),
		FailedRequests: m.FailedRequests.Load(),
	}
}

// MockServerStats contains server statistics.
type MockServerStats struct {
	TotalRequests  int64
	BytesServed    int64
	RangeRequests  int64
	FullRequests   int64
	FailedRequests int64
}

func (m *MockServer) handleRequest(w http.ResponseWriter, r *http.Request) {
	if m.CustomHandler != nil {
		m.CustomHandler(w, r)
		return
	}

	m.RequestCount.Add(1)
	m.ActiveRequests.Add(1)
	defer m.ActiveRequests.Add(-1)

	// Track request number for fail-on-nth logic
	m.requestCountMu.Lock()
	m.internalReqNum++
	reqNum := m.internalReqNum
	m.requestCountMu.Unlock()

	// Check max concurrent requests
	if m.MaxConcurrentReqs > 0 && m.ActiveRequests.Load() > int64(m.MaxConcurrentReqs) {
		m.FailedRequests.Add(1)
		http.Error(w, "Too many requests", http.StatusTooManyRequests)
		return
	}

	// Fail on Nth request if configured
	if m.FailOnNthRequest > 0 && reqNum == m.FailOnNthRequest {
		m.FailedRequests.Add(1)
		http.Error(w, "Simulated failure", http.StatusInternalServerError)
		return
	}

	// Add request latency
	if m.Latency > 0 {
		time.Sleep(m.Latency)
	}

	// Handle HEAD requests for probing
	if r.Method == http.MethodHead {
		m.setCommonHeaders(w, 0, m.FileSize-1)
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusOK)
		return
	}

	// Parse Range header
	rangeHeader := r.Header.Get("Range")
	start := int64(0)
	end := m.FileSize - 1

	if rangeHeader != "" && m.SupportsRanges {
		m.RangeRequests.Add(1)

		// Parse "bytes=start-end"
		var err error
		start, end, err = parseRange(rangeHeader, m.FileSize)
		if err != nil {
			http.Error(w, "Invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}

		m.setCommonHeaders(w, start, end)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, m.FileSize))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		m.FullRequests.Add(1)
		m.setCommonHeaders(w, 0, m.FileSize-1)
		if m.SupportsRanges {
			w.Header().Set("Accept-Ranges", "bytes")
		}
		w.WriteHeader(http.StatusOK)
	}

	// Serve data
	length := end - start + 1
	bytesWritten := int64(0)

	// Write in chunks to support byte latency and fail-after-bytes
	chunkSize := int64(32 * 1024) // 32KB chunks
	for bytesWritten < length {
		// Check if we should fail (per-request byte count, not global)
		// This allows retry logic to work - new requests can succeed
		if m.FailAfterBytes > 0 && bytesWritten >= m.FailAfterBytes {
			m.FailedRequests.Add(1)
			// Abruptly close connection by not writing more
			return
		}

		remaining := length - bytesWritten
		if remaining < chunkSize {
			chunkSize = remaining
		}

		dataStart := start + bytesWritten
		dataEnd := dataStart + chunkSize
		if dataEnd > m.FileSize {
			dataEnd = m.FileSize
		}

		n, err := w.Write(m.data[dataStart:dataEnd])
		if err != nil {
			return // Client disconnected
		}

		bytesWritten += int64(n)
		m.BytesServed.Add(int64(n))

		// Add byte latency
		if m.ByteLatency > 0 {
			time.Sleep(m.ByteLatency * time.Duration(n))
		}
	}
}

func (m *MockServer) setCommonHeaders(w http.ResponseWriter, start, end int64) {
	w.Header().Set("Content-Type", m.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	if m.Filename != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, m.Filename))
	}
}

// parseRange parses an HTTP Range header and returns start, end positions.
// Handles formats like "bytes=0-499" or "bytes=500-"
func parseRange(rangeHeader string, fileSize int64) (int64, int64, error) {
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return 0, 0, fmt.Errorf("invalid range prefix")
	}

	rangeSpec := strings.TrimPrefix(rangeHeader, "bytes=")
	parts := strings.Split(rangeSpec, "-")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid range format")
	}

	var start, end int64
	var err error

	if parts[0] == "" {
		// Suffix range: -500 means last 500 bytes
		end = fileSize - 1
		start, err = strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			return 0, 0, err
		}
		start = fileSize - start
	} else {
		start, err = strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			return 0, 0, err
		}

		if parts[1] == "" {
			// Open-ended range: 500-
			end = fileSize - 1
		} else {
			end, err = strconv.ParseInt(parts[1], 10, 64)
			if err != nil {
				return 0, 0, err
			}
		}
	}

	// Validate
	if start < 0 || end >= fileSize || start > end {
		return 0, 0, fmt.Errorf("range out of bounds")
	}

	return start, end, nil
}

// StreamingMockServer provides a variant that generates data on-the-fly
// instead of pre-allocating, useful for very large file simulations.
type StreamingMockServer struct {
	*MockServer
}

// NewStreamingMockServer creates a mock server that streams generated data.
func NewStreamingMockServer(fileSize int64, opts ...MockServerOption) *StreamingMockServer {
	// Create base server with minimal data
	m := &MockServer{
		FileSize:       fileSize,
		SupportsRanges: true,
		ContentType:    "application/octet-stream",
		Filename:       "testfile.bin",
		RandomData:     false,
	}

	for _, opt := range opts {
		opt(m)
	}

	// Only allocate a small buffer for streaming
	m.data = make([]byte, 64*1024) // 64KB buffer
	if m.RandomData {
		_, _ = rand.Read(m.data)
	}

	s := &StreamingMockServer{MockServer: m}
	m.Server = NewHTTPServer(http.HandlerFunc(s.handleStreamingRequest))
	return s
}

// NewStreamingMockServerT creates a streaming mock server and skips the test if binding fails.
func NewStreamingMockServerT(t *testing.T, fileSize int64, opts ...MockServerOption) *StreamingMockServer {
	t.Helper()
	m := &MockServer{
		FileSize:       fileSize,
		SupportsRanges: true,
		ContentType:    "application/octet-stream",
		Filename:       "testfile.bin",
		RandomData:     false,
	}

	for _, opt := range opts {
		opt(m)
	}

	m.data = make([]byte, 64*1024)
	if m.RandomData {
		_, _ = rand.Read(m.data)
	}

	s := &StreamingMockServer{MockServer: m}
	m.Server = NewHTTPServerT(t, http.HandlerFunc(s.handleStreamingRequest))
	return s
}

func (s *StreamingMockServer) handleStreamingRequest(w http.ResponseWriter, r *http.Request) {
	s.RequestCount.Add(1)
	s.ActiveRequests.Add(1)
	defer s.ActiveRequests.Add(-1)

	// Track request number for fail-on-nth logic
	s.requestCountMu.Lock()
	s.internalReqNum++
	reqNum := s.internalReqNum
	s.requestCountMu.Unlock()

	// Fail on Nth request if configured
	if s.FailOnNthRequest > 0 && reqNum == s.FailOnNthRequest {
		s.FailedRequests.Add(1)
		http.Error(w, "Simulated failure", http.StatusInternalServerError)
		return
	}

	// Add request latency
	if s.Latency > 0 {
		time.Sleep(s.Latency)
	}

	// Handle HEAD requests
	if r.Method == http.MethodHead {
		s.setCommonHeaders(w, 0, s.FileSize-1)
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusOK)
		return
	}

	// Parse Range header
	rangeHeader := r.Header.Get("Range")
	start := int64(0)
	end := s.FileSize - 1

	if rangeHeader != "" && s.SupportsRanges {
		s.RangeRequests.Add(1)
		var err error
		start, end, err = parseRange(rangeHeader, s.FileSize)
		if err != nil {
			http.Error(w, "Invalid range", http.StatusRequestedRangeNotSatisfiable)
			return
		}

		s.setCommonHeaders(w, start, end)
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, s.FileSize))
		w.WriteHeader(http.StatusPartialContent)
	} else {
		s.FullRequests.Add(1)
		s.setCommonHeaders(w, 0, s.FileSize-1)
		if s.SupportsRanges {
			w.Header().Set("Accept-Ranges", "bytes")
		}
		w.WriteHeader(http.StatusOK)
	}

	// Stream data using the small buffer
	length := end - start + 1
	bytesWritten := int64(0)
	bufLen := int64(len(s.data))

	for bytesWritten < length {
		remaining := length - bytesWritten
		chunkSize := bufLen
		if remaining < chunkSize {
			chunkSize = remaining
		}

		n, err := w.Write(s.data[:chunkSize])
		if err != nil {
			return
		}

		bytesWritten += int64(n)
		s.BytesServed.Add(int64(n))

		// Add byte latency
		if s.ByteLatency > 0 {
			time.Sleep(s.ByteLatency * time.Duration(n) / 1024) // Per KB
		}
	}
}

// DownloadResult represents the result of a download operation for testing.
type DownloadResult struct {
	Error       error
	BytesRead   int64
	Duration    time.Duration
	Resumed     bool
	Connections int
}

// AssertDownloadSuccess is a test helper that checks download success.
func AssertDownloadSuccess(result DownloadResult, expectedBytes int64) error {
	if result.Error != nil {
		return fmt.Errorf("download failed: %w", result.Error)
	}
	if result.BytesRead != expectedBytes {
		return fmt.Errorf("expected %d bytes, got %d", expectedBytes, result.BytesRead)
	}
	return nil
}
