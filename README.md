<div align="center">

# Surge

[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/surge-downloader/surge)
[![Release](https://img.shields.io/github/v/release/surge-downloader/surge?style=flat-square&color=blue)](https://github.com/surge-downloader/surge/releases)
[![Go Version](https://img.shields.io/github/go-mod/go-version/surge-downloader/surge?style=flat-square&color=cyan)](go.mod)
[![License](https://img.shields.io/badge/License-MIT-grey.svg?style=flat-square)](LICENSE)
[![X (formerly Twitter) Follow](https://img.shields.io/twitter/follow/SurgeDownloader?style=social)](https://x.com/SurgeDownloader)
[![Stars](https://img.shields.io/github/stars/surge-downloader/surge?style=social)](https://github.com/surge-downloader/surge/stargazers)

**Blazing fast, open-source TUI download manager built in Go.**

[Installation](#installation) • [Usage](#usage) • [Benchmarks](#benchmarks) • [Extension](#browser-extension)

</div>

---

## What is Surge?

Surge is designed for power users who prefer a keyboard-driven workflow. It features a beautiful **Terminal User Interface (TUI)**, as well as a background **Headless Server** and a **CLI tool** for automation.

![Surge Demo](assets/demo.gif)

## Why use Surge?

Most browsers open a single connection for a download. Surge opens multiple (up to 32), splits the file, and downloads chunks in parallel. But we take it a step further:

- **Smart "Work Stealing":** If a fast worker finishes its chunk, it doesn't sit idle. It "steals" work from slower workers to ensure the download finishes as fast as physics allows.
- **Multiple Mirrors:** Download from multiple sources simultaneously. Surge distributes workers across all available mirrors and automatically handles failover.
- **Slow Worker Restart:** We monitor mean speeds. If a worker is lagging (< 0.3x average), Surge kills it and restarts the connection to find a faster route.
- **Sequential Download:** Option to download files in strict order (Streaming Mode). Ideal for media files that you want to preview while downloading.
- **Daemon Architecture:** Surge runs a single background "engine." You can open 10 different terminal tabs and queue downloads; they all funnel into one efficient manager.
- **Beautiful TUI:** Built with Bubble Tea & Lipgloss, it looks good while it works.

---

## Installation

### Option 1: Prebuilt Binaries (Easiest)

Download the latest binary for your OS from the [Releases Page](https://github.com/surge-downloader/surge/releases/latest).

### Option 2: Install with AUR

```bash
yay -S surge
```

### Option 3: Homebrew (macOS/Linux)

```bash
brew install surge-downloader/tap/surge
```

### Option 4: Go Install

```bash
go install github.com/surge-downloader/surge@latest
```

---

## Usage

Surge has two main modes: **TUI (Interactive)** and **Server (Headless)**.

### 1. Interactive TUI Mode

Just run `surge` to enter the dashboard. This is where you can visualize progress, manage the queue, and see the speed graphs.

```bash
# Start the TUI
surge

# Start TUI with downloads queued
surge https://example.com/file1.zip https://example.com/file2.zip

# Start with multiple mirrors (comma-separated or multiple arguments)
surge https://mirror1.com/file.zip,https://mirror2.com/file.zip

# Combine URLs and batch file
surge https://example.com/file.zip --batch urls.txt

# Start without resuming paused downloads
surge --no-resume

# Auto-exit when all downloads complete
surge https://example.com/file.zip --exit-when-done
```

### 2. Server Mode (Headless)

Great for servers, Raspberry Pis, or background processes.

```bash
# Start the server
surge server start

# Start the server with a download
surge server start https://url.com/file.zip,https://mirror1.com/file.zip,https://mirror2.com/file.zip

# Start on a specific port with options
surge server start --port 8090 --no-resume

# Check server status
surge server status

# Stop the server
surge server stop
```

### 3. Remote TUI (Connect to a Daemon)

Use this when Surge is running on another machine (or a local daemon you started with `surge server start`).

```bash
# Connect to a local daemon (auto-discovery via ~/.surge/port)
surge connect

# Connect to a remote daemon
surge connect 192.168.1.10:1700 --token <token>

# Or set the token once in the environment
export SURGE_TOKEN=<token>
surge connect 192.168.1.10:1700
```

Notes:
- The daemon requires a token for all API calls. Print it with `surge token` on the server host.
- Remote TUI is a viewer/controller for the daemon state; the daemon owns resume behavior.

### 3. Command Reference

All other commands can be used to interact with a running Surge instance (TUI or Server).

| Command  | Alias  | Description                 | Usage Examples                                        |
| :------- | :----- | :-------------------------- | :---------------------------------------------------- |
| `add`    | `get`  | Add a download to the queue | `surge add <url>`<br>`surge add --batch urls.txt`     |
| `ls`     | `l`    | List all downloads          | `surge ls`<br>`surge ls --watch`<br>`surge ls --json` |
| `pause`  | -      | Pause a download            | `surge pause <id>`<br>`surge pause --all`             |
| `resume` | -      | Resume a download           | `surge resume <id>`<br>`surge resume --all`           |
| `rm`     | `kill` | Remove/Cancel a download    | `surge rm <id>`<br>`surge rm --clean`                 |

> **Note:** IDs can be partial (e.g., first 4-8 characters) as long as they are unique.

---

## Benchmarks

We tested Surge against standard tools. Because of our connection optimization logic, Surge significantly outperforms single-connection tools.

| Tool      | Time       | Speed          | Comparison   |
| --------- | ---------- | -------------- | ------------ |
| **Surge** | **28.93s** | **35.40 MB/s** | **—**        |
| aria2c    | 40.04s     | 25.57 MB/s     | 1.38× slower |
| curl      | 57.57s     | 17.79 MB/s     | 1.99× slower |
| wget      | 61.81s     | 16.57 MB/s     | 2.14× slower |

> _Test details: 1GB file, Windows 11, Ryzen 5 5600X, 360 Mbps Network. Results averaged over 5 runs._

We would love to see you benchmark surge on your system!

---

## Browser Extension

The Surge extension intercepts browser downloads and sends them straight to your terminal. It communicates with the Surge client on port **1700** by default.

### Chrome / Edge / Brave

1.  Clone or download this repository.
2.  Open your browser and navigate to `chrome://extensions`.
3.  Enable **"Developer mode"** in the top right corner.
4.  Click **"Load unpacked"**.
5.  Select the `extension-chrome` folder from the `surge` directory.

### Firefox

1.  **Stable:** [Get the Add-on](https://addons.mozilla.org/en-US/firefox/addon/surge/)
2.  **Development:**
    - Navigate to `about:debugging#/runtime/this-firefox`.
    - Click **"Load Temporary Add-on..."**.
    - Select the `manifest.json` file inside the `extension-firefox` folder.

### Connection & Troubleshooting

- Ensure Surge is running (either TUI `surge` or Server `surge server start`).
- The extension icon should show a green dot when connected.
- If the dot is red, check if Surge is running and listening on port 1700.
- **Auth required:** the daemon now protects all API endpoints. In the extension popup, paste the token from `surge token` and click **Save**.
- If downloads are not intercepted, make sure **Intercept Downloads** is enabled in the popup.
- The extension ignores `blob:` / `data:` URLs and historical downloads (older than ~30s).
- Chrome debugging: open `chrome://extensions` → Surge → **Service worker** → **Inspect** for logs and errors.
- Firefox debugging: `about:debugging#/runtime/this-firefox` → Surge → **Inspect**.

---

## Community & Contributing

We love community contributions! Whether it's a bug fix, a new feature, or just cleaning up typos.

You can check out the [Discussions](https://github.com/surge-downloader/surge/discussions) for any questions or ideas, or follow us on [X (Twitter)](https://x.com/SurgeDownloader)!

## License

Distributed under the MIT License. See [LICENSE](https://github.com/surge-downloader/surge/blob/main/LICENSE) for more information.

---

<div align="center">
<a href="https://star-history.com/#surge-downloader/surge&Date">
 <picture>
   <source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/svg?repos=surge-downloader/surge&type=Date&theme=dark" />
   <source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/svg?repos=surge-downloader/surge&type=Date" />
   <img alt="Star History Chart" src="https://api.star-history.com/svg?repos=surge-downloader/surge&type=Date" />
 </picture>
</a>
  
<br />
If Surge saved you some time, consider giving it a ⭐ to help others find it!
</div>
