# Settings & Configuration

This document provides a detailed overview of all configuration options and CLI commands available in Surge.

## Configuration File

Surge stores its configuration in a `settings.json` file located in the application data directory:
- **Windows:** `%APPDATA%\surge\settings.json`
- **macOS:** `~/Library/Application Support/surge/settings.json`
- **Linux:** `~/.config/surge/settings.json`

### General Settings
| Key | Type | Description | Default |
| :--- | :--- | :--- | :--- |
| `default_download_dir` | string | Directory where new downloads are saved. If empty, defaults to `~/Downloads` or current directory. | `""` |
| `warn_on_duplicate` | bool | Show a warning when adding a download that already exists in the list. | `true` |
| `extension_prompt` | bool | Prompt for confirmation in the TUI when adding downloads via the browser extension. | `false` |
| `auto_resume` | bool | Automatically resume paused downloads when Surge starts. | `false` |
| `skip_update_check` | bool | Disable automatic check for new versions on startup. | `false` |
| `clipboard_monitor` | bool | Watch the system clipboard for URLs and prompt to download them. | `true` |
| `theme` | int | UI Theme (0=Adaptive, 1=Light, 2=Dark). | `0` |
| `log_retention_count` | int | Number of recent log files to keep. | `5` |

### Connection Settings
| Key | Type | Description | Default |
| :--- | :--- | :--- | :--- |
| `max_connections_per_host` | int | Maximum concurrent connections allowed to a single host (1-64). | `32` |
| `max_global_connections` | int | Maximum total concurrent connections across all active downloads. | `100` |
| `max_concurrent_downloads` | int | Maximum number of downloads running simultaneously (requires restart). | `3` |
| `user_agent` | string | Custom User-Agent string for HTTP requests. Leave empty for default. | `""` |
| `proxy_url` | string | HTTP/HTTPS proxy URL (e.g., `http://127.0.0.1:8080`). Leave empty to use system settings. | `""` |
| `sequential_download` | bool | Download file pieces in strict order (Streaming Mode). Useful for previewing media but may be slower. | `false` |

### Chunk Settings
| Key | Type | Description | Default |
| :--- | :--- | :--- | :--- |
| `min_chunk_size` | int64 | Minimum size of a download chunk in bytes (e.g., `2097152` for 2MB). | `2MB` |
| `worker_buffer_size` | int | I/O buffer size per worker in bytes (e.g., `524288` for 512KB). | `512KB` |

### Performance Settings
| Key | Type | Description | Default |
| :--- | :--- | :--- | :--- |
| `max_task_retries` | int | Number of times to retry a failed chunk before giving up. | `3` |
| `slow_worker_threshold` | float | Restart workers slower than this fraction of the mean speed (0.0-1.0). | `0.3` |
| `slow_worker_grace_period` | duration | Time to wait before checking a worker's speed (e.g., `5s`). | `5s` |
| `stall_timeout` | duration | Restart workers that haven't received data for this duration (e.g., `3s`). | `3s` |
| `speed_ema_alpha` | float | Exponential moving average smoothing factor for speed calculation (0.0-1.0). | `0.3` |

---

## CLI Reference

Surge provides a robust Command Line Interface for automation and scripting.

### `surge [url...]`
Start the interactive TUI mode. If URLs are provided, they are added to the queue immediately.

**Flags:**
- `--batch, -b <file>`: Read URLs from a file (one per line).
- `--port, -p <port>`: Force the internal server to listen on a specific port.
- `--output, -o <dir>`: Set a default output directory for this session.
- `--no-resume`: Do not auto-resume paused downloads on startup.
- `--exit-when-done`: Automatically exit the application when all downloads complete.

### `surge add <url>`
Add a download to the running instance (or start a new one if not running).

**Flags:**
- `--batch, -b <file>`: Add multiple URLs from a file.
- `--output, -o <dir>`: Specify the output directory for this download.

### `surge connect [host]`
Connect the TUI to a remote Surge daemon.

**Flags:**
- `--token <token>`: Bearer token for authentication (or set `SURGE_TOKEN` env var).
- `--insecure-http`: Allow plain HTTP connections to non-loopback targets.

### `surge ls`
List all downloads in the queue.

**Flags:**
- `--json`: Output the list in JSON format (useful for scripts).
- `--watch`: Watch mode (refresh every second).

### `surge pause <id>`
Pause a specific download by ID (or partial ID).

**Flags:**
- `--all`: Pause all active downloads.

### `surge resume <id>`
Resume a specific paused download by ID.

**Flags:**
- `--all`: Resume all paused downloads.

### `surge rm <id>`
Remove/Cancel a download.

**Flags:**
- `--clean`: Remove all completed downloads from the list.

### `surge server start`
Start Surge in headless server mode (no TUI). Ideal for background services or remote servers.

**Flags:**
- `--batch, -b <file>`: Load initial URLs from a file.
- `--port, -p <port>`: Listen on a specific port.
- `--output, -o <dir>`: Set the default output directory.
- `--exit-when-done`: Exit when the queue is empty.
- `--no-resume`: Do not auto-resume paused downloads on startup.
