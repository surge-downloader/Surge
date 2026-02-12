package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/utils"
)

var lsCmd = &cobra.Command{
	Use:     "ls [id]",
	Aliases: []string{"l"},
	Short:   "List downloads",
	Long:    `List all downloads from the running server or database. Optionally show details for a specific download by ID.`,
	Args:    cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		initializeGlobalState()

		jsonOutput, _ := cmd.Flags().GetBool("json")
		watch, _ := cmd.Flags().GetBool("watch")

		// If ID provided, show details for that download
		if len(args) == 1 {
			showDownloadDetails(args[0], jsonOutput)
			return
		}

		if watch {
			for {
				// Clear screen first for watch mode
				fmt.Print("\033[H\033[2J")
				printDownloads(jsonOutput)
				time.Sleep(1 * time.Second)
			}
		} else {
			printDownloads(jsonOutput)
		}
	},
}

// downloadInfo is a unified structure for display
type downloadInfo struct {
	ID         string  `json:"id"`
	URL        string  `json:"url,omitempty"`
	Filename   string  `json:"filename"`
	Status     string  `json:"status"`
	Progress   float64 `json:"progress"`
	TotalSize  int64   `json:"total_size"`
	Downloaded int64   `json:"downloaded"`
	Speed      float64 `json:"speed,omitempty"`
}

func printDownloads(jsonOutput bool) {
	var downloads []downloadInfo

	// Try to get from running server first
	port := readActivePort()
	if port > 0 {
		serverDownloads, err := GetRemoteDownloads(port)
		if err == nil {
			for _, s := range serverDownloads {
				downloads = append(downloads, downloadInfo{
					ID:         s.ID,
					Filename:   s.Filename,
					Status:     s.Status,
					Progress:   s.Progress,
					TotalSize:  s.TotalSize,
					Downloaded: s.Downloaded,
					Speed:      s.Speed,
				})
			}
		}
	}

	// If no server running or no active downloads, fall back to database
	if len(downloads) == 0 {
		dbDownloads, err := state.ListAllDownloads()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing downloads: %v\n", err)
			os.Exit(1)
		}

		for _, d := range dbDownloads {
			var progress float64
			if d.TotalSize > 0 {
				progress = float64(d.Downloaded) * 100 / float64(d.TotalSize)
			}
			downloads = append(downloads, downloadInfo{
				ID:         d.ID,
				Filename:   d.Filename,
				Status:     d.Status,
				Progress:   progress,
				TotalSize:  d.TotalSize,
				Downloaded: d.Downloaded,
			})
		}
	}

	if len(downloads) == 0 {
		if !jsonOutput {
			fmt.Println("No downloads found.")
		} else {
			fmt.Println("[]")
		}
		return
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(downloads, "", "  ")
		fmt.Println(string(data))
		return
	}

	// Table output
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tFILENAME\tSTATUS\tPROGRESS\tSPEED\tSIZE")
	_, _ = fmt.Fprintln(w, "--\t--------\t------\t--------\t-----\t----")

	for _, d := range downloads {
		progress := fmt.Sprintf("%.1f%%", d.Progress)
		size := utils.ConvertBytesToHumanReadable(d.TotalSize)

		// Speed display
		var speed string
		if d.Speed > 0 {
			speed = fmt.Sprintf("%.1f MB/s", d.Speed)
		} else {
			speed = "-"
		}

		// Truncate ID for display
		id := d.ID
		if len(id) > 8 {
			id = id[:8]
		}

		// Truncate filename
		filename := d.Filename
		if len(filename) > 25 {
			filename = filename[:22] + "..."
		}

		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", id, filename, d.Status, progress, speed, size)
	}
	_ = w.Flush()
}

func showDownloadDetails(partialID string, jsonOutput bool) {
	// Resolve partial ID
	fullID, err := resolveDownloadID(partialID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Try to get from running server first
	port := readActivePort()
	if port > 0 {
		resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/download?id=%s", port, fullID))
		if err == nil {
			defer func() {
				if err := resp.Body.Close(); err != nil {
					utils.Debug("Error closing response body: %v", err)
				}
			}()
			if resp.StatusCode == http.StatusOK {
				var status types.DownloadStatus
				if json.NewDecoder(resp.Body).Decode(&status) == nil {
					printDownloadDetail(status, jsonOutput)
					return
				}
			}
		}
	}

	// Fall back to database - search through all downloads
	downloads, err := state.ListAllDownloads()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error listing downloads: %v\n", err)
		os.Exit(1)
	}

	var found *types.DownloadEntry
	for _, d := range downloads {
		if d.ID == fullID {
			found = &d
			break
		}
	}

	if found == nil {
		fmt.Fprintf(os.Stderr, "Error: download not found: %s\n", partialID)
		os.Exit(1)
	}

	var progress float64
	if found.TotalSize > 0 {
		progress = float64(found.Downloaded) * 100 / float64(found.TotalSize)
	}

	status := types.DownloadStatus{
		ID:         found.ID,
		URL:        found.URL,
		Filename:   found.Filename,
		Status:     found.Status,
		TotalSize:  found.TotalSize,
		Downloaded: found.Downloaded,
		Progress:   progress,
	}
	printDownloadDetail(status, jsonOutput)
}

func printDownloadDetail(d types.DownloadStatus, jsonOutput bool) {
	if jsonOutput {
		data, _ := json.MarshalIndent(d, "", "  ")
		fmt.Println(string(data))
		return
	}

	fmt.Printf("ID:         %s\n", d.ID)
	fmt.Printf("URL:        %s\n", d.URL)
	fmt.Printf("Filename:   %s\n", d.Filename)
	fmt.Printf("Status:     %s\n", d.Status)
	fmt.Printf("Progress:   %.1f%%\n", d.Progress)
	fmt.Printf("Downloaded: %s / %s\n", utils.ConvertBytesToHumanReadable(d.Downloaded), utils.ConvertBytesToHumanReadable(d.TotalSize))
	if d.Speed > 0 {
		fmt.Printf("Speed:      %.1f MB/s\n", d.Speed)
	}
	if d.Error != "" {
		fmt.Printf("Error:      %s\n", d.Error)
	}
}

func init() {
	rootCmd.AddCommand(lsCmd)
	lsCmd.Flags().Bool("json", false, "Output in JSON format")
	lsCmd.Flags().Bool("watch", false, "Watch mode: refresh every second")
}
