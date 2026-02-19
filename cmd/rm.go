package cmd

import (
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/spf13/cobra"
	"github.com/surge-downloader/surge/internal/engine/state"
	"github.com/surge-downloader/surge/internal/utils"
)

var rmCmd = &cobra.Command{
	Use:     "rm <ID>",
	Aliases: []string{"kill"},
	Short:   "Remove a download",
	Long:    `Remove a download by its ID. Use --clean to remove all completed downloads.`,
	Args:    cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		initializeGlobalState()

		clean, _ := cmd.Flags().GetBool("clean")

		if !clean && len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Error: provide a download ID or use --clean")
			os.Exit(1)
		}

		if clean {
			// Remove completed downloads from DB
			count, err := state.RemoveCompletedDownloads()
			if err != nil {
				fmt.Fprintf(os.Stderr, "Error cleaning downloads: %v\n", err)
				os.Exit(1)
			}
			fmt.Printf("Removed %d completed downloads.\n", count)
			return
		}

		baseURL, token, err := resolveAPIConnection(true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		id := args[0]

		// Resolve partial ID to full ID
		id, err = resolveDownloadID(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Send to running server
		path := fmt.Sprintf("/delete?id=%s", url.QueryEscape(id))
		resp, err := doAPIRequest(http.MethodPost, baseURL, token, path, nil)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error connecting to server: %v\n", err)
			os.Exit(1)
		}
		defer func() {
			if err := resp.Body.Close(); err != nil {
				utils.Debug("Error closing response body: %v", err)
			}
		}()

		if resp.StatusCode != http.StatusOK {
			fmt.Fprintf(os.Stderr, "Error: server returned %s\n", resp.Status)
			os.Exit(1)
		}
		fmt.Printf("Removed download %s\n", id[:8])
	},
}

func init() {
	rootCmd.AddCommand(rmCmd)
	rmCmd.Flags().Bool("clean", false, "Remove all completed downloads")
}
