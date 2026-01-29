package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/surge-downloader/surge/internal/config"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/engine/types"
)

// readActivePort reads the port from the port file
func readActivePort() int {
	portFile := filepath.Join(config.GetSurgeDir(), "port")
	data, err := os.ReadFile(portFile)
	if err != nil {
		return 0
	}
	var port int
	fmt.Sscanf(string(data), "%d", &port)
	return port
}

// readURLsFromFile reads URLs from a file, one per line
func readURLsFromFile(filepath string) ([]string, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	var urls []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" && !strings.HasPrefix(line, "#") {
			urls = append(urls, line)
		}
	}
	return urls, scanner.Err()
}

// sendToServer sends a download request to a running surge server
func sendToServer(url, outPath string, port int) error {
	reqBody := DownloadRequest{
		URL:  url,
		Path: outPath,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	serverURL := fmt.Sprintf("http://127.0.0.1:%d/download", port)
	resp, err := http.Post(serverURL, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error: %s - %s", resp.Status, string(body))
	}

	// Optional: Print response info (ID etc) if needed, but usually caller handles success msg
	// Or we can parse ID here and return it?
	// The caller (add.go/root.go) might want to know ID.
	// For now, keep it simple as error/nil.

	var respData map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&respData) // Ignore error? safely
	if id, ok := respData["id"].(string); ok {
		// Could log debug
		_ = id
	}

	return nil
}

// GetRemoteDownloads fetches all downloads from the running server
func GetRemoteDownloads(port int) ([]types.DownloadStatus, error) {
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/list", port))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status: %s", resp.Status)
	}

	var statuses []types.DownloadStatus
	if err := json.NewDecoder(resp.Body).Decode(&statuses); err != nil {
		return nil, err
	}

	return statuses, nil
}

// resolveDownloadID resolves a partial ID (prefix) to a full download ID.
// If the input is at least 8 characters and matches a single download, returns the full ID.
// Returns the original ID if no match found or if it's already a full ID.
func resolveDownloadID(partialID string) (string, error) {
	if len(partialID) >= 32 {
		return partialID, nil // Already a full UUID
	}

	var candidates []string

	// 1. Try to get candidates from running server
	port := readActivePort()
	if port > 0 {
		remoteDownloads, err := GetRemoteDownloads(port)
		if err == nil {
			for _, d := range remoteDownloads {
				candidates = append(candidates, d.ID)
			}
		}
	}

	// 2. Get all downloads from database
	downloads, err := state.ListAllDownloads()
	if err == nil {
		for _, d := range downloads {
			candidates = append(candidates, d.ID)
		}
	} else if port == 0 {
		// Only return error if we couldn't check server AND db failed
		return partialID, nil
	}

	// Find matches among all candidates
	var matches []string
	seen := make(map[string]bool)

	for _, id := range candidates {
		if strings.HasPrefix(id, partialID) {
			if !seen[id] {
				matches = append(matches, id)
				seen[id] = true
			}
		}
	}

	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("ambiguous ID prefix '%s' matches %d downloads", partialID, len(matches))
	}

	return partialID, nil // No match, use as-is (will fail with "not found" later)
}
