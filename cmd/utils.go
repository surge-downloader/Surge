package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/surge-downloader/surge/internal/config"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/source"
	"github.com/surge-downloader/surge/internal/utils"
)

// readActivePort reads the port from the port file
func readActivePort() int {
	portFile := filepath.Join(config.GetRuntimeDir(), "port")
	data, err := os.ReadFile(portFile)
	if err != nil {
		return 0
	}
	var port int
	_, _ = fmt.Sscanf(string(data), "%d", &port)
	return port
}

// readURLsFromFile reads URLs from a file, one per line
func readURLsFromFile(filepath string) ([]string, error) {
	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = file.Close() }()

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

// ParseURLArg parses a command line argument that might contain comma-separated mirrors
// Returns the primary URL and a list of all mirrors (including the primary)
func ParseURLArg(arg string) (string, []string) {
	return source.ParseCommaArg(arg)
}

func resolveLocalToken() string {
	if token := strings.TrimSpace(globalToken); token != "" {
		return token
	}
	if token := strings.TrimSpace(os.Getenv("SURGE_TOKEN")); token != "" {
		return token
	}
	return ensureAuthToken()
}

func resolveHostTarget() string {
	if host := strings.TrimSpace(globalHost); host != "" {
		return host
	}
	return strings.TrimSpace(os.Getenv("SURGE_HOST"))
}

func resolveAPIConnection(requireServer bool) (string, string, error) {
	target := resolveHostTarget()
	if target == "" {
		port := readActivePort()
		if port > 0 {
			return fmt.Sprintf("http://127.0.0.1:%d", port), resolveLocalToken(), nil
		}
		if !requireServer {
			return "", "", nil
		}
		return "", "", errors.New("surge is not running locally. start it or pass --host (or set SURGE_HOST)")
	}

	baseURL, err := resolveConnectBaseURL(target, false)
	if err != nil {
		return "", "", err
	}
	token, err := resolveTokenForTarget(target)
	if err != nil {
		return "", "", err
	}
	return baseURL, token, nil
}

func doAPIRequest(method string, baseURL string, token string, path string, body io.Reader) (*http.Response, error) {
	reqURL := fmt.Sprintf("%s%s", strings.TrimRight(baseURL, "/"), path)
	req, err := http.NewRequest(method, reqURL, body)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{}
	return client.Do(req)
}

func sendToServer(url string, mirrors []string, outPath string, baseURL string, token string) error {
	reqBody := DownloadRequest{
		URL:     url,
		Mirrors: mirrors,
		Path:    outPath,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("failed to marshal request: %w", err)
	}

	resp, err := doAPIRequest(http.MethodPost, baseURL, token, "/download", bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			utils.Debug("Error closing response body: %v", err)
		}
	}()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error: %s - %s", resp.Status, string(body))
	}

	return nil
}

// GetRemoteDownloads fetches all downloads from the running server
func GetRemoteDownloads(baseURL string, token string) ([]types.DownloadStatus, error) {
	resp, err := doAPIRequest(http.MethodGet, baseURL, token, "/list", nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			utils.Debug("Error closing response body: %v", err)
		}
	}()

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

	strictRemote := resolveHostTarget() != ""
	var candidates []string

	// 1. Try to get candidates from running server
	baseURL, token, err := resolveAPIConnection(false)
	if err != nil {
		return "", err
	}
	if baseURL != "" {
		remoteDownloads, err := GetRemoteDownloads(baseURL, token)
		if err != nil {
			if strictRemote {
				return "", fmt.Errorf("failed to list remote downloads: %w", err)
			}
		} else {
			appendCandidateIDs(&candidates, remoteDownloads)
		}
	}

	if strictRemote {
		return resolveIDFromCandidates(partialID, candidates)
	}

	// 2. Get all downloads from database
	downloads, err := state.ListAllDownloads()
	if err == nil {
		for _, d := range downloads {
			candidates = append(candidates, d.ID)
		}
	} else if len(candidates) == 0 {
		// Only short-circuit when both remote and DB are unavailable.
		return partialID, nil
	}

	return resolveIDFromCandidates(partialID, candidates)
}

func appendCandidateIDs(candidates *[]string, downloads []types.DownloadStatus) {
	for _, d := range downloads {
		*candidates = append(*candidates, d.ID)
	}
}

func resolveIDFromCandidates(partialID string, candidates []string) (string, error) {
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
