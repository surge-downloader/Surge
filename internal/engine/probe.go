package engine

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/utils"
)

var probeClient = &http.Client{Timeout: types.ProbeTimeout}

var ua = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) " +
	"AppleWebKit/537.36 (KHTML, like Gecko) " +
	"Chrome/120.0.0.0 Safari/537.36"

// ProbeResult contains all metadata from server probe
type ProbeResult struct {
	FileSize      int64
	SupportsRange bool
	Filename      string
	ContentType   string
}

// ProbeServer sends GET with Range: bytes=0-0 to determine server capabilities
// headers is optional - pass nil for non-authenticated probes
func ProbeServer(ctx context.Context, rawurl string, filenameHint string, headers map[string]string) (*ProbeResult, error) {
	utils.Debug("Probing server: %s", rawurl)

	var resp *http.Response
	var err error

	// Create a client that preserves headers on redirects (for authenticated downloads)
	client := &http.Client{
		Timeout: types.ProbeTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			// Copy headers from original request to redirect request
			if len(via) > 0 {
				for key, vals := range via[0].Header {
					if key == "Range" {
						continue
					}
					req.Header[key] = vals
				}
			}
			return nil
		},
	}

	// Retry logic for probe request
	for i := 0; i < 3; i++ {
		if i > 0 {
			time.Sleep(1 * time.Second)
			utils.Debug("Retrying probe... attempt %d", i+1)
		}

		probeCtx, cancel := context.WithTimeout(ctx, types.ProbeTimeout)
		defer cancel()

		req, reqErr := http.NewRequestWithContext(probeCtx, http.MethodGet, rawurl, nil)
		if reqErr != nil {
			err = fmt.Errorf("failed to create probe request: %w", reqErr)
			break // Fatal error, don't retry
		}

		// Apply custom headers first (from browser extension: cookies, auth, etc.)
		for key, val := range headers {
			if key != "Range" { // Skip Range, we set our own
				req.Header.Set(key, val)
			}
		}

		req.Header.Set("Range", "bytes=0-0")
		// Set User-Agent only if not provided in custom headers
		if req.Header.Get("User-Agent") == "" {
			req.Header.Set("User-Agent", ua)
		}

		resp, err = client.Do(req)
		if err == nil {
			break // Success
		}
	}

	if err != nil {
		return nil, fmt.Errorf("probe request failed after retries: %w", err)
	}

	defer func() {
		io.Copy(io.Discard, resp.Body) // Drain any remaining data
		resp.Body.Close()
	}()

	utils.Debug("Probe response status: %d", resp.StatusCode)

	result := &ProbeResult{}

	// Determine range support and file size based on status code
	switch resp.StatusCode {
	case http.StatusPartialContent: // 206
		result.SupportsRange = true
		// Parse Content-Range: bytes 0-0/TOTAL
		contentRange := resp.Header.Get("Content-Range")
		utils.Debug("Content-Range header: %s", contentRange)
		if contentRange != "" {
			// Format: "bytes 0-0/12345" or "bytes 0-0/*"
			if idx := strings.LastIndex(contentRange, "/"); idx != -1 {
				sizeStr := contentRange[idx+1:]
				if sizeStr != "*" {
					result.FileSize, _ = strconv.ParseInt(sizeStr, 10, 64)
				}
			}
		}
		utils.Debug("Range supported, file size: %d", result.FileSize)

	case http.StatusOK: // 200 - server ignores Range header
		result.SupportsRange = false
		contentLength := resp.Header.Get("Content-Length")
		if contentLength != "" {
			result.FileSize, _ = strconv.ParseInt(contentLength, 10, 64)
		}
		utils.Debug("Range NOT supported (got 200), file size: %d", result.FileSize)

	default:
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Determine filename using strengthened logic
	name, _, err := utils.DetermineFilename(rawurl, resp, false)
	if err != nil {
		utils.Debug("Error determining filename: %v", err)
		name = "download.bin"
	}

	if filenameHint != "" {
		result.Filename = filenameHint
	} else {
		result.Filename = name
	}

	result.ContentType = resp.Header.Get("Content-Type")

	utils.Debug("Probe complete - filename: %s, size: %d, range: %v",
		result.Filename, result.FileSize, result.SupportsRange)

	return result, nil
}

// ProbeMirrors concurrently checks a list of mirrors and returns valid ones and errors
func ProbeMirrors(ctx context.Context, mirrors []string) (valid []string, errors map[string]error) {
	// Deduplicate
	unique := make(map[string]bool)
	for _, m := range mirrors {
		unique[m] = true
	}

	var candidates []string
	for m := range unique {
		candidates = append(candidates, m)
	}

	utils.Debug("Probing %d mirrors...", len(candidates))

	valid = make([]string, 0, len(candidates))
	errors = make(map[string]error)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, url := range candidates {
		wg.Add(1)
		go func(target string) {
			defer wg.Done()

			// Short timeout for bulk probing
			probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()

			result, err := ProbeServer(probeCtx, target, "", nil)

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				errors[target] = err
				return
			}

			if result.SupportsRange {
				valid = append(valid, target)
			} else {
				errors[target] = fmt.Errorf("does not support ranges")
			}
		}(url)
	}

	wg.Wait()
	utils.Debug("Mirror probing complete: %d valid, %d failed", len(valid), len(errors))
	return valid, errors
}
