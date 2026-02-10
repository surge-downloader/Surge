package cmd

import (
	"context"
	"fmt"
	"net"
	"net/url"
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

		insecureHTTP, _ := cmd.Flags().GetBool("insecure-http")
		baseURL, err := resolveConnectBaseURL(target, insecureHTTP)
		if err != nil {
			fmt.Println(err.Error())
			os.Exit(1)
		}

		// Resolve token
		tokenFlag, _ := cmd.Flags().GetString("token")
		token := strings.TrimSpace(tokenFlag)
		if token == "" {
			// Allow env override
			token = strings.TrimSpace(os.Getenv("SURGE_TOKEN"))
		}
		if token == "" {
			// Only reuse local token for loopback targets.
			host := target
			if idx := strings.Index(host, ":"); idx != -1 {
				host = host[:idx]
			}
			if host == "127.0.0.1" || host == "localhost" {
				token = ensureAuthToken()
			} else {
				fmt.Println("No token provided. Use --token or set SURGE_TOKEN.")
				os.Exit(1)
			}
		}

		fmt.Printf("Connecting to %s...\n", baseURL)

		// Create Remote Service
		service := core.NewRemoteDownloadService(baseURL, token)

		// Verify connection
		_, err = service.List()
		if err != nil {
			fmt.Printf("Failed to connect: %v\n", err)
			os.Exit(1)
		}

		// Event loop
		stream, cleanup, err := service.StreamEvents(context.Background())
		if err != nil {
			fmt.Printf("Failed to start event stream: %v\n", err)
			os.Exit(1)
		}
		defer cleanup()

		// Parse port for display
		port := 0
		parts := strings.Split(target, ":")
		if len(parts) == 2 {
			port, _ = strconv.Atoi(parts[1])
		}

		// Initialize TUI
		// Using false for noResume because resume logic is handled by the server (remote service)
		// we just want to reflect the state.
		m := tui.InitialRootModel(port, Version, service, false)

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
	connectCmd.Flags().String("token", "", "Bearer token for remote daemon (or set SURGE_TOKEN)")
	connectCmd.Flags().Bool("insecure-http", false, "Allow plain HTTP for non-loopback targets")
	rootCmd.AddCommand(connectCmd)
}

func resolveConnectBaseURL(target string, allowInsecureHTTP bool) (string, error) {
	if strings.Contains(target, "://") {
		u, err := url.Parse(target)
		if err != nil {
			return "", fmt.Errorf("Invalid target: %v", err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return "", fmt.Errorf("Unsupported scheme %q (use http or https)", u.Scheme)
		}
		if u.Host == "" {
			return "", fmt.Errorf("Invalid target: missing host")
		}
		if u.Scheme == "http" && !allowInsecureHTTP && !isLoopbackHost(u.Hostname()) {
			return "", fmt.Errorf("Refusing insecure HTTP for non-loopback target. Use https:// or --insecure-http")
		}
		return fmt.Sprintf("%s://%s", u.Scheme, u.Host), nil
	}

	scheme := "https"
	if isLoopbackHost(hostnameFromTarget(target)) {
		scheme = "http"
	}
	return fmt.Sprintf("%s://%s", scheme, target), nil
}

func hostnameFromTarget(target string) string {
	host := target
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}
	return host
}

func isLoopbackHost(host string) bool {
	if host == "" {
		return false
	}
	h := strings.ToLower(host)
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}
