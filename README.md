# Surge

[![Build Status](https://github.com/JunaidIslam2105/surge/actions/workflows/ci.yml/badge.svg)](https://github.com/JunaidIslam2105/surge/actions)
[![Release](https://img.shields.io/github/v/release/JunaidIslam2105/surge)](https://github.com/JunaidIslam2105/surge/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/JunaidIslam2105/surge)](go.mod)

## About

Surge is a blazing fast, open-source terminal (TUI) download manager built in Go. Designed for power users who prefer a keyboard-driven workflow and want full control over their downloads.

![demo](assets/demo.gif)

## Installation

### Go Install

```bash
go install github.com/JunaidIslam2105/surge@latest
```

### Build from Source

```bash
git clone https://github.com/JunaidIslam2105/surge.git
cd surge
go build -o surge .
```

### Prebuilt Binaries

Download the latest release from [GitHub Releases](https://github.com/JunaidIslam2105/surge/releases).

## Features

- **High-speed downloads** with multi-connection support
- **Beautiful TUI** built with Bubble Tea & Lipgloss
- **Pause/Resume** downloads seamlessly
- **Real-time progress** with speed graphs and ETA
- **Auto-retry** on connection failures
- **Smart file detection** and organization
- **Browser extension** integration

## Usage

```bash
# Start TUI mode
surge

# Headless download (CLI only, no TUI)
surge get <URL>

# Headless download with custom output directory
surge get <URL> -o ~/Downloads
```

## Benchmarks

| Tool | Time | Speed | vs Surge |
|------|------|-------|----------|
| **Surge** | 28.93s | **35.40 MB/s** | — |
| aria2c | 40.04s | 25.57 MB/s | 1.38× slower |
| curl | 57.57s | 17.79 MB/s | 1.99× slower |
| wget | 61.81s | 16.57 MB/s | 2.14× slower |

<details>
<summary>Test Environment</summary>

*Results averaged over 5 runs*

| | |
|---|---|
| **File** | 1 GB ([Hetzner Speed Test](https://speed.hetzner.com/)) |
| **OS** | Windows 11 Pro |
| **CPU** | AMD Ryzen 5 5600X |
| **RAM** | 16 GB DDR4 |
| **Network** | 360 Mbps / 45 MB/s |

Run your own: `python benchmark.py -n 5`
</details>

## Browser Extension

Intercept downloads from your browser and send them directly to Surge.

### Installation

1. Open Chrome/Edge and navigate to `chrome://extensions`
2. Enable **Developer mode**
3. Click **Load unpacked** and select the `extension` folder
4. Ensure Surge is running before downloading

The extension will automatically intercept downloads and send them to Surge via `http://127.0.0.1`.

## Contributing

Contributions are welcome! Feel free to fork, make changes, and submit a pull request.

## License

This project is licensed under the MIT License see the [LICENSE](LICENSE) file for details.
