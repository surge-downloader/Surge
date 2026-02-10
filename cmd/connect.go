package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/surge-downloader/surge/internal/core"
)

var connectCmd = &cobra.Command{
	Use:   "connect [host:port]",
	Short: "Connect TUI to a running Surge daemon",
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var target string
		if len(args) > 0 {
			target = args[0]
		} else {
			// Auto-discovery from local port file
			port := readActivePort()
			if port > 0 {
				target = fmt.Sprintf("127.0.0.1:%d", port)
			} else {
				fmt.Println("No active Surge daemon found locally.")
				fmt.Println("Usage: surge connect <host:port>")
				os.Exit(1)
			}
		}

		// Ensure target has scheme
		baseURL := "http://" + target

		// Load or prompt for token
		token := ensureAuthToken() // For now, we reuse the local token file. In future, prompt if remote.

		fmt.Printf("Connecting to %s...\n", baseURL)

		// Create Remote Service
		service := core.NewRemoteDownloadService(baseURL, token)

		// Verify connection
		_, err := service.List()
		if err != nil {
			fmt.Printf("Failed to connect: %v\n", err)
			os.Exit(1)
		}

		// Initialize TUI with Remote Service
		// Note: We need to update StartTUI or InitialRootModel to accept the service
		// For Phase 2 verification, we will just print success and maybe listen to events briefly
		// Phase 3 will do the full TUI integration.

		fmt.Println("Connection successful!")
		fmt.Println("Streaming events (Press Ctrl+C to stop)...")

		// Event loop
		stream, err := service.StreamEvents()
		if err != nil {
			fmt.Printf("Failed to start event stream: %v\n", err)
			os.Exit(1)
		}

		for msg := range stream {
			// Print event type and basic info
			fmt.Printf("Event: %T\n", msg)
		}
	},
}

func init() {
	rootCmd.AddCommand(connectCmd)
}
