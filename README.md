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

## Backstory

We are two college students who maintain this project. We suffered from terrible college internet and desperately wanted something that downloads files as fast as physically possible.

The existing options were lacking: XDM is no longer maintained, and IDM is closed-source and paid. So, we built Surge—an open-source, high-performance download manager that squeezes every bit of bandwidth out of your connection.

## Why use Surge?

Most browsers open a single connection for a download. Surge opens multiple (up to 32), splits the file, and downloads chunks in parallel. But we take it a step further:

- **Blazing Fast:** Designed to maximize your bandwidth utilization and download files as quickly as possible.
- **Multiple Mirrors:** Download from multiple sources simultaneously. Surge distributes workers across all available mirrors and automatically handles failover.
- **Sequential Download:** Option to download files in strict order (Streaming Mode). Ideal for media files that you want to preview while downloading.
- **Daemon Architecture:** Surge runs a single background "engine." You can open 10 different terminal tabs and queue downloads; they all funnel into one efficient manager.
- **Beautiful TUI:** Built with Bubble Tea & Lipgloss, it looks good while it works.

For a deep dive into how we make downloads faster (like work stealing and slow worker handling), check out our **[Optimization Guide](docs/OPTIMIZATIONS.md)**.

---

## Installation

Surge is available on multiple platforms. Choose the method that works best for you.

| Platform / Method | Command / Instructions | Notes |
| :--- | :--- | :--- |
| **Prebuilt Binary** | [Download from Releases](https://github.com/surge-downloader/surge/releases/latest) | Easiest method. Just download and run. |
| **Arch Linux (AUR)** | `yay -S surge` | Managed via AUR. |
| **macOS / Linux (Homebrew)** | `brew install surge-downloader/tap/surge` | Recommended for Mac/Linux users. |
| **Windows (Winget)** | `winget install surge-downloader.surge` | Recommended for Windows users. |
| **Go Install** | `go install github.com/surge-downloader/surge@latest` | Requires Go 1.21+. |

---

## Usage

Surge has two main modes: **TUI (Interactive)** and **Server (Headless)**.

For a comprehensive list of all commands, flags, and configuration options, please refer to the **[Detailed Settings & Configuration Guide](docs/SETTINGS.md)**.

### 1. Interactive TUI Mode

Just run `surge` to enter the dashboard. This is where you can visualize progress, manage the queue, and see speed graphs.

```bash
# Start the TUI
surge

# Start TUI with downloads queued
surge https://example.com/file1.zip https://example.com/file2.zip

# Combine URLs and batch file
surge https://example.com/file.zip --batch urls.txt
```

### 2. Server Mode (Headless)

Great for servers, Raspberry Pis, or background processes.

```bash
# Start the server
surge server start

# Start the server with a download
surge server start https://url.com/file.zip

# Check server status
surge server status
```

### 3. Remote TUI

Connect to a running Surge daemon (local or remote).

```bash
# Connect to a local daemon (auto-discovery)
surge connect

# Connect to a remote daemon
surge connect 192.168.1.10:1700 --token <token>
```

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
