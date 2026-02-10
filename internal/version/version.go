// Package version provides functionality for checking for Surge updates via GitHub API.
package version

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	// GitHubAPIURL is the endpoint for fetching the latest release
	GitHubAPIURL = "https://api.github.com/repos/surge-downloader/surge/releases/latest"
	// RequestTimeout is the timeout for the GitHub API request
	RequestTimeout = 10 * time.Second
)

// UpdateInfo contains information about an available update
type UpdateInfo struct {
	CurrentVersion  string // The current version of Surge
	LatestVersion   string // The latest version available on GitHub
	ReleaseURL      string // URL to the GitHub release page
	UpdateAvailable bool   // Whether an update is available
}

// GitHubRelease represents the relevant fields from the GitHub API response
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

// CheckForUpdate checks if a newer version of Surge is available on GitHub.
// Returns nil, nil if there's a network error (fail silently).
// Returns UpdateInfo with UpdateAvailable=false if current version is up to date.
// Returns UpdateInfo with UpdateAvailable=true if a newer version exists.
func CheckForUpdate(currentVersion string) (*UpdateInfo, error) {
	// Skip check for development builds
	if currentVersion == "dev" || currentVersion == "" {
		return nil, nil
	}

	client := &http.Client{
		Timeout: RequestTimeout,
	}

	req, err := http.NewRequest("GET", GitHubAPIURL, nil)
	if err != nil {
		return nil, nil // Fail silently
	}

	// Set User-Agent as required by GitHub API
	req.Header.Set("User-Agent", "Surge-Update-Checker")
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, nil // Network error - fail silently
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, nil // API error - fail silently
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, nil // Parse error - fail silently
	}

	latestVersion := normalizeVersion(release.TagName)
	currentNormalized := normalizeVersion(currentVersion)

	updateInfo := &UpdateInfo{
		CurrentVersion:  currentVersion,
		LatestVersion:   release.TagName,
		ReleaseURL:      release.HTMLURL,
		UpdateAvailable: isNewerVersion(latestVersion, currentNormalized),
	}

	return updateInfo, nil
}

// normalizeVersion removes the 'v' prefix and trims whitespace
func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	return version
}

// isNewerVersion compares two semver strings and returns true if latest > current
// Assumes format: MAJOR.MINOR.PATCH (e.g., "1.2.3")
func isNewerVersion(latest, current string) bool {
	latestParts := parseVersion(latest)
	currentParts := parseVersion(current)

	for i := 0; i < 3; i++ {
		if latestParts[i] > currentParts[i] {
			return true
		}
		if latestParts[i] < currentParts[i] {
			return false
		}
	}
	return false // Versions are equal
}

// parseVersion parses a semver string into [major, minor, patch]
func parseVersion(version string) [3]int {
	var parts [3]int

	// Split by '.' and parse each part
	segments := strings.Split(version, ".")
	for i := 0; i < len(segments) && i < 3; i++ {
		// Parse the numeric part (ignore any suffix like "-beta")
		numStr := segments[i]
		if idx := strings.IndexAny(numStr, "-+"); idx != -1 {
			numStr = numStr[:idx]
		}
		_, _ = fmt.Sscanf(numStr, "%d", &parts[i])
	}

	return parts
}
