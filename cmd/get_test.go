package cmd

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/surge-downloader/surge/internal/core"
	"github.com/surge-downloader/surge/internal/download"
	"github.com/surge-downloader/surge/internal/engine/types"
)

func TestCLI_NewEndpoints(t *testing.T) {
	// Initialize GlobalPool for tests
	GlobalProgressCh = make(chan any, 100)
	GlobalPool = download.NewWorkerPool(GlobalProgressCh, 4)

	// Create listener
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	// Start server in background
	svc := core.NewLocalDownloadService(GlobalPool)
	go startHTTPServer(ln, port, "", svc)
	time.Sleep(50 * time.Millisecond)

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)

	// Add a dummy download to the pool to test against
	id := "test-id"
	GlobalPool.Add(types.DownloadConfig{
		ID:  id,
		URL: "http://example.com/test",
		State: &types.ProgressState{
			ID: id,
		},
	})
	// Wait a tiny bit for the worker to pick it up (though Add queues it)
	time.Sleep(10 * time.Millisecond)

	t.Run("Pause Endpoint", func(t *testing.T) {
		resp, err := http.Post(baseURL+"/pause?id="+id, "application/json", nil)
		if err != nil {
			t.Fatalf("Failed to request pause: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
		}

		var result map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if result["status"] != "paused" {
			t.Errorf("Expected status 'paused', got %v", result["status"])
		}
	})

	t.Run("Resume Endpoint", func(t *testing.T) {
		resp, err := http.Post(baseURL+"/resume?id="+id, "application/json", nil)
		if err != nil {
			t.Fatalf("Failed to request resume: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
		}

		var result map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if result["status"] != "resumed" {
			t.Errorf("Expected status 'resumed', got %v", result["status"])
		}
	})

	t.Run("Delete Endpoint", func(t *testing.T) {
		resp, err := http.Post(baseURL+"/delete?id="+id, "application/json", nil)
		if err != nil {
			t.Fatalf("Failed to request delete: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusOK {
			t.Errorf("Expected 200 OK, got %d", resp.StatusCode)
		}

		var result map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}
		if result["status"] != "deleted" {
			t.Errorf("Expected status 'deleted', got %v", result["status"])
		}
	})

	t.Run("Delete Missing ID Endpoint", func(t *testing.T) {
		resp, err := http.Post(baseURL+"/delete", "application/json", nil) // Missing ID
		if err != nil {
			t.Fatalf("Failed request: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected 400 Bad Request for missing ID, got %d", resp.StatusCode)
		}
	})
}
