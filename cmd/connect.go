package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/surge-downloader/surge/internal/core"
	"github.com/surge-downloader/surge/internal/tui"
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

		// Event loop
		stream, err := service.StreamEvents()
		if err != nil {
			fmt.Printf("Failed to start event stream: %v\n", err)
			os.Exit(1)
		}

		// Parse port for display
		port := 0
		parts := strings.Split(target, ":")
		if len(parts) == 2 {
			port, _ = strconv.Atoi(parts[1])
		}

		// Initialize TUI
		// Using false for noResume because resume logic is handled by the server (remote service)
		// we just want to reflect the state.
		m := tui.InitialRootModel(port, Version, service, stream, false)

		p := tea.NewProgram(m, tea.WithAltScreen())

		// Pipe events to program
		go func() {
			for msg := range stream {
				p.Send(msg)
			}
		}()

		// Run TUI
		if _, err := p.Run(); err != nil {
			fmt.Printf("Error running TUI: %v\n", err)
			os.Exit(1)
		}
	},
}

func init() {
	rootCmd.AddCommand(connectCmd)
}
