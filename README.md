# Surge

[![Release](https://img.shields.io/github/v/release/junaid2005p/surge)](https://github.com/junaid2005p/surge/releases)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/junaid2005p/surge)](go.mod)

## About

Surge is a blazing fast, open-source terminal (TUI) download manager built in Go. Designed for power users who prefer a keyboard-driven workflow and want full control over their downloads.

![demo](assets/demo.gif)

## Installation

### Prebuilt Binaries

[![Get it on GitHub](https://img.shields.io/badge/Get%20it%20on-GitHub-blue?style=for-the-badge&logo=github)](https://github.com/junaid2005p/surge/releases/latest)

### Go Install

```bash
go install github.com/junaid2005p/surge@latest
```

### Build from Source

```bash
git clone https://github.com/junaid2005p/surge.git
cd surge
go build -o surge .
```

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

# Batch download from a file (one URL per line)
surge get --batch urls.txt -o ~/Downloads
```

### Batch Downloads

Import multiple URLs at once from a text file (one URL per line). Duplicate URLs are automatically detected and skipped.

- **TUI**: Press `B` to import a batch file
- **CLI**: Use `surge get --batch urls.txt`

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
| **File** | 1GB.bin ([link](https://sin-speed.hetzner.com/1GB.bin)) |
| **OS** | Windows 11 Pro |
| **CPU** | AMD Ryzen 5 5600X |
| **RAM** | 16 GB DDR4 |
| **Network** | 360 Mbps / 45 MB/s |

Run your own: `python benchmark.py -n 5`
</details>

## Browser Extension

Intercept downloads from your browser and send them directly to Surge.

### Chrome / Edge

1. Navigate to `chrome://extensions`
2. Enable **Developer mode**
3. Click **Load unpacked** and select the `extension` folder
4. Ensure Surge is running before downloading

### Firefox

1. Navigate to `about:debugging`
2. Click **This Firefox** in the sidebar
3. Click **Load Temporary Add-on...**
4. Select `manifest.json` from the `extension-firefox` folder

> **Note:** Temporary add-ons are removed when Firefox closes. For permanent installation, the extension must be signed via [addons.mozilla.org](https://addons.mozilla.org).

The extension will automatically intercept downloads and send them to Surge via `http://127.0.0.1`.

## Contributing

Contributions are welcome! Feel free to fork, make changes, and submit a pull request.

## License

If you find Surge useful, consider giving it a ⭐ it helps others discover the project!

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
