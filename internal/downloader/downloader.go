package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"surge/internal/util"
)

type Downloader struct {
	Client                   *http.Client //Every downloader has a http client over which the downloads happen
	bytesDownloadedPerSecond []int64
	mu                       sync.Mutex
}

func NewDownloader() *Downloader {
	client := http.Client{
		Timeout: 0,
	}
	return &Downloader{Client: &client}
}

func (d *Downloader) Download(ctx context.Context, rawurl, outPath string, concurrent int, verbose bool) error {
	if concurrent > 1 {
		return d.concurrentDownload(ctx, rawurl, outPath, concurrent, verbose)
	}
	return d.singleDownload(ctx, rawurl, outPath, verbose)
}

func (d *Downloader) singleDownload(ctx context.Context, rawurl, outPath string, verbose bool) error {
	parsed, err := url.Parse(rawurl) //Parses the URL into parts
	if err != nil {
		return err
	}

	if parsed.Scheme == "" {
		return errors.New("url missing scheme (use http:// or https://)")
	} //if the URL does not have a scheme, return an error

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil) //We use a context so that we can cancel the download whenever we want
	if err != nil {
		return err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) "+
		"AppleWebKit/537.36 (KHTML, like Gecko) "+
		"Chrome/120.0.0.0 Safari/537.36") // We set a browser like header to avoid being blocked by some websites

	resp, err := d.Client.Do(req) //Exectes the HTTP request
	if err != nil {
		return err
	}
	defer resp.Body.Close() //Closes the response body when the function returns

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
    return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	filename := filepath.Base(outPath)
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		// naive parsing: look for filename="..."
		if idx := strings.Index(cd, "filename="); idx != -1 {
			name := cd[idx+len("filename="):]
			name = strings.Trim(name, `"' `)
			if name != "" {
				filename = filepath.Base(name)
			}
		}
	}

	outDir := filepath.Dir(outPath)
	tmpFile, err := os.CreateTemp(outDir, filename+".part.*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()

	defer func() {
		tmpFile.Close()
		// if download failed, remove temp file
		if err != nil {
			os.Remove(tmpPath)
		}
	}()

	var total int64 = -1
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if v, e := strconv.ParseInt(cl, 10, 64); e == nil {
			total = v
		}
	}

	// copy loop with manual buffering so we can measure progress
	buf := make([]byte, 32*1024) // 32KB buffer
	var written int64 = 0
	lastReport := time.Now()
	start := time.Now()

	for {
		// respect context cancellation: check before read
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			wn, werr := tmpFile.Write(buf[:n])
			if werr != nil {
				return werr
			}
			if wn != n {
				return io.ErrShortWrite
			}
			written += int64(n)
		}

		// progress reporting periodically (every 200ms or on finish)
		now := time.Now()
		if now.Sub(lastReport) > 200*time.Millisecond || readErr == io.EOF {
			d.printProgress(written, total, start, verbose)
			lastReport = now
		}

		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return readErr
		}
	}

	// sync file to disk
	if err := tmpFile.Sync(); err != nil {
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	// atomically move temp to dest
	destPath := outPath
	if info, err := os.Stat(outPath); err == nil && info.IsDir() {
		// When outPath is a directory we must have a valid filename.
		// The filename variable was determined earlier. It might be invalid if derived from a directory name
		if filename == "" || filename == "." || filename == "/" {
			// Try to get it from URL as a last resort
			filename = filepath.Base(parsed.Path)
			if filename == "" || filename == "." || filename == "/" {
				return fmt.Errorf("could not determine filename to save in directory %s", outPath)
			}
		}
		destPath = filepath.Join(outPath, filename)
	}

	if renameErr := os.Rename(tmpPath, destPath); renameErr != nil {
		// fallback: copy if rename fails across filesystems
		in, rerr := os.Open(tmpPath)
		if rerr == nil {
			out, werr := os.Create(destPath)
			if werr == nil {
				_, _ = io.Copy(out, in)
				out.Close()
			}
			in.Close()
		}
		os.Remove(tmpPath)
		return fmt.Errorf("rename failed: %v", renameErr)
	}

	elapsed := time.Since(start)
	speed := float64(written) / 1024.0 / elapsed.Seconds() // KiB/s
	fmt.Fprintf(os.Stderr, "\nDownloaded %s in %s (%s/s)\n", destPath, elapsed.Round(time.Second), util.ConvertBytesToHumanReadable(int64(speed*1024)))
	return nil
}

func (d *Downloader) concurrentDownload(ctx context.Context, rawurl, outPath string, concurrent int, verbose bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return err
	}

	resp, err := d.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.Header.Get("Accept-Ranges") != "bytes" {
		fmt.Println("Server does not support concurrent download, falling back to single thread")
		return d.singleDownload(ctx, rawurl, outPath, verbose)
	}

	totalSize, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	if err != nil {
		return err
	}

	chunkSize := totalSize / int64(concurrent)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var written int64

	startTime := time.Now()

	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		start := int64(i) * chunkSize
		end := start + chunkSize - 1
		if i == concurrent-1 {
			end = totalSize - 1
		}

		go func(i int, start, end int64) {
			defer wg.Done()
			err := d.downloadChunk(ctx, rawurl, outPath, i, start, end, &mu, &written, totalSize, startTime, verbose)
			if err != nil {
				fmt.Fprintf(os.Stderr, "\nError downloading chunk %d: %v\n", i, err)
			}
		}(i, start, end)
	}

	wg.Wait()

	fmt.Print("Downloaded all parts! Merging...\n")

	// Merge files
	destFile, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer destFile.Close()

	for i := 0; i < concurrent; i++ {
		partFileName := fmt.Sprintf("%s.part%d", outPath, i)
		partFile, err := os.Open(partFileName)
		if err != nil {
			return err
		}
		_, err = io.Copy(destFile, partFile)
		if err != nil {
			partFile.Close()
			return err
		}
		partFile.Close()
		os.Remove(partFileName)
	}

	elapsed := time.Since(startTime)
	speed := float64(totalSize) / 1024.0 / elapsed.Seconds() // KiB/s
	fmt.Fprintf(os.Stderr, "\nDownloaded %s in %s (%s/s)\n", outPath, elapsed.Round(time.Second), util.ConvertBytesToHumanReadable(int64(speed*1024)))
	return nil
}

func (d *Downloader) downloadChunk(ctx context.Context, rawurl, outPath string, index int, start, end int64, mu *sync.Mutex, written *int64, totalSize int64, startTime time.Time, verbose bool) error {

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawurl, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, end))
	resp, err := d.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	partFileName := fmt.Sprintf("%s.part%d", outPath, index)
	partFile, err := os.Create(partFileName)
	if err != nil {
		return err
	}
	defer partFile.Close()

	buf := make([]byte, 32*1024)
	for {

		n, err := resp.Body.Read(buf)

		if n > 0 {
			_, wErr := partFile.Write(buf[:n])
			if wErr != nil {
				return wErr
			}
			mu.Lock()
			*written += int64(n)
			d.printProgress(*written, totalSize, startTime, verbose)
			mu.Unlock()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (d *Downloader) printProgress(written, total int64, start time.Time, verbose bool) {
	elapsed := time.Since(start).Seconds()
	speed := float64(written) / 1024.0 / elapsed // KiB/s

	d.mu.Lock()
	d.bytesDownloadedPerSecond = append(d.bytesDownloadedPerSecond, int64(speed))
	if len(d.bytesDownloadedPerSecond) > 30 {
		d.bytesDownloadedPerSecond = d.bytesDownloadedPerSecond[1:]
	}

	var avgSpeed float64
	var totalSpeed int64
	for _, s := range d.bytesDownloadedPerSecond {
		totalSpeed += s
	}
	if len(d.bytesDownloadedPerSecond) > 0 {
		avgSpeed = float64(totalSpeed) / float64(len(d.bytesDownloadedPerSecond))
	}
	d.mu.Unlock()

	eta := "N/A"
	if total > 0 && avgSpeed > 0 {
		remainingBytes := total - written
		remainingSeconds := float64(remainingBytes) / (avgSpeed * 1024)
		eta = time.Duration(remainingSeconds * float64(time.Second)).Round(time.Second).String()
	}

	if total > 0 {
		pct := float64(written) / float64(total) * 100.0
		fmt.Fprintf(os.Stderr, "\r%.2f%% %s/%s (%.1f KiB/s) ETA: %s", pct, util.ConvertBytesToHumanReadable(written), util.ConvertBytesToHumanReadable(total), speed, eta)
	} else {
		fmt.Fprintf(os.Stderr, "\r%s (%.1f KiB/s)", util.ConvertBytesToHumanReadable(written), speed)
	}
}
