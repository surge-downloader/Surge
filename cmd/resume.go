package cmd

import (
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/spf13/cobra"
	"github.com/surge-downloader/surge/internal/utils"
)

var resumeCmd = &cobra.Command{
	Use:   "resume <ID>",
	Short: "Resume a paused download",
	Long:  `Resume a paused download by its ID. Use --all to resume all paused downloads.`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		initializeGlobalState()

		all, _ := cmd.Flags().GetBool("all")

		if !all && len(args) == 0 {
			fmt.Fprintln(os.Stderr, "Error: provide a download ID or use --all")
			os.Exit(1)
		}

		baseURL, token, err := resolveAPIConnection(true)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		if all {
			fmt.Println("Resuming all downloads is not yet implemented for running server.")
			return
		}

		id := args[0]

		// Resolve partial ID to full ID
		id, err = resolveDownloadID(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}

		// Send to running server
		path := fmt.Sprintf("/resume?id=%s", url.QueryEscape(id))
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
		fmt.Printf("Resumed download %s\n", id[:8])
	},
}

func init() {
	rootCmd.AddCommand(resumeCmd)
	resumeCmd.Flags().Bool("all", false, "Resume all paused downloads")
}
