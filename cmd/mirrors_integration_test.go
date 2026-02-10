package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/surge-downloader/surge/internal/testutil"
)

// TestMirrors_CLI_Integration verifies that the processDownloads function (used by CLI)
// correctly parses comma-separated mirrors and sends them to the server.
func TestMirrors_CLI_Integration(t *testing.T) {
	// 1. Start a mock Surge server
	receivedRequest := make(chan DownloadRequest, 1)

	server := testutil.NewHTTPServerT(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/download" {
			http.Error(w, "Not found", http.StatusNotFound)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("Failed to read body: %v", err)
			return
		}

		var req DownloadRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("Failed to parse JSON: %v", err)
			return
		}

		receivedRequest <- req
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, `{"status":"queued"}`)
	}))
	defer server.Close()

	// Extract port from the mock server URL
	_, portStr, _ := net.SplitHostPort(server.Listener.Addr().String())
	var port int
	_, _ = fmt.Sscanf(portStr, "%d", &port)

	// 2. Call processDownloads with a URL containing mirrors
	primaryURL := "http://example.com/file.zip"
	mirror1 := "http://mirror1.com/file.zip"
	mirror2 := "http://mirror2.com/file.zip"

	// Input format: "url,mirror1,mirror2"
	arg := fmt.Sprintf("%s,%s,%s", primaryURL, mirror1, mirror2)

	// Simulate "surge add <arg>"
	processDownloads([]string{arg}, ".", port)

	// 3. Verify the server received the correct request
	select {
	case req := <-receivedRequest:
		if req.URL != primaryURL {
			t.Errorf("Expected URL %q, got %q", primaryURL, req.URL)
		}

		if len(req.Mirrors) != 3 {
			t.Fatalf("Expected 3 mirrors (including primary), got %d", len(req.Mirrors))
		}

		// Verify mirror contents
		expectedMirrors := []string{primaryURL, mirror1, mirror2}
		for i, m := range req.Mirrors {
			if m != expectedMirrors[i] {
				t.Errorf("Mirror[%d] mismatch: expected %q, got %q", i, expectedMirrors[i], m)
			}
		}

	case <-receivedRequest:
		// success
	default:
		t.Error("Server did not receive request")
	}
}

// TestParseURLArg_Unit tests the parsing logic directly
func TestParseURLArg_Unit(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		expectedURL     string
		expectedMirrors []string
	}{
		{
			name:            "Single URL",
			input:           "http://test.com/file",
			expectedURL:     "http://test.com/file",
			expectedMirrors: []string{"http://test.com/file"},
		},
		{
			name:            "URL with one mirror",
			input:           "http://a.com,http://b.com",
			expectedURL:     "http://a.com",
			expectedMirrors: []string{"http://a.com", "http://b.com"},
		},
		{
			name:            "URL with spaces",
			input:           "http://a.com , http://b.com",
			expectedURL:     "http://a.com",
			expectedMirrors: []string{"http://a.com", "http://b.com"},
		},
		{
			name:            "Empty URL",
			input:           "",
			expectedURL:     "",
			expectedMirrors: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url, mirrors := ParseURLArg(tt.input)

			if url != tt.expectedURL {
				t.Errorf("URL: expected %q, got %q", tt.expectedURL, url)
			}

			if len(mirrors) != len(tt.expectedMirrors) {
				t.Errorf("Mirrors: expected %d, got %d", len(tt.expectedMirrors), len(mirrors))
			}

			for i := range mirrors {
				if mirrors[i] != tt.expectedMirrors[i] {
					t.Errorf("Mirror[%d]: expected %q, got %q", i, tt.expectedMirrors[i], mirrors[i])
				}
			}
		})
	}
}
