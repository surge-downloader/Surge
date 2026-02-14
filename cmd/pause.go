package cmd

import (
	"fmt"
	"net/http"
	"os"

	"github.com/spf13/cobra"
	"github.com/surge-downloader/surge/internal/utils"
)

var pauseCmd = &cobra.Command{
	Use:   "pause <ID>",
	Short: "Pause a download",
	Long:  `Pause a download by its ID. Use --all to pause all downloads.`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		initializeGlobalState()

		all, _ := cmd.Flags().GetBool("all")

		if !all && len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Error: provide a download ID or use --all")
			os.Exit(1)
		}

		port := readActivePort()
		if port == 0 {
			fmt.Fprintln(os.Stderr, "Error: Surge should be running.")
			fmt.Fprintln(os.Stderr, "Start it with 'surge server start' and try again.")
			os.Exit(1)
		}

		if all {
			// TODO: Implement /pause-all endpoint or iterate
			fmt.Println("Pausing all downloads is not yet implemented for running server.")
			return
		}

		id := args[0]

		// Resolve partial ID to full ID
		id, err := resolveDownloadID(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Send to running server
		resp, err := http.Post(fmt.Sprintf("http://127.0.0.1:%d/pause?id=%s", port, id), "application/json", nil)
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
		fmt.Printf("Paused download %s\n", id[:8])
	},
}

func init() {
	rootCmd.AddCommand(pauseCmd)
	pauseCmd.Flags().Bool("all", false, "Pause all downloads")
}
