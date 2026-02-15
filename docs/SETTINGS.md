# Settings & Configuration

This document provides a detailed overview of all configuration options and CLI commands available in Surge.

## Configuration File

You can **access the settings in TUI** or if you prefer
from the `settings.json` file located in the application data directory:
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
| `max_concurrent_downloads` | int | Maximum number of downloads running simultaneously (requires restart). | `3` |
| `user_agent` | string | Custom User-Agent string for HTTP requests. Leave empty for default. | `""` |
| `proxy_url` | string | HTTP/HTTPS proxy URL (e.g., `http://127.0.0.1:8080`). Leave empty to use system settings. | `""` |
| `sequential_download` | bool | Download file pieces in strict order (Streaming Mode). Useful for previewing media but may be slower. | `false` |
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

### Command Table

| Command | What it does | Key flags | Notes |
| :--- | :--- | :--- | :--- |
| `surge [url]...` | Launches local TUI. Queues optional URLs. | `--batch, -b`<br>`--port, -p`<br>`--output, -o`<br>`--no-resume`<br>`--exit-when-done` | If `--host` is set, this becomes remote TUI mode. |
| `surge server [url]...` | Launches headless server. Queues optional URLs. | `--batch, -b`<br>`--port, -p`<br>`--output, -o`<br>`--exit-when-done`<br>`--no-resume`<br>`--token` | Primary headless mode command. |
| `surge connect <host:port>` | Launches TUI connected to remote server. | `--insecure-http` | Convenience alias for remote TUI usage. |
| `surge add <url>...` | Queues downloads via CLI/API. | `--batch, -b`<br>`--output, -o` | Alias: `get`. |
| `surge ls [id]` | Lists downloads, or shows one download detail. | `--json`<br>`--watch` | Alias: `l`. |
| `surge pause <id>` | Pauses a download by ID/prefix. | `--all` | |
| `surge resume <id>` | Resumes a paused download by ID/prefix. | `--all` | |
| `surge rm <id>` | Removes a download by ID/prefix. | `--clean` | Alias: `kill`. |
| `surge token` | Prints current API auth token. | None | Useful for remote clients. |

### Server Subcommands (Compatibility)

| Command | What it does |
| :--- | :--- |
| `surge server start [url]...` | Legacy equivalent of `surge server [url]...`. |
| `surge server stop` | Stops a running server process by PID file. |
| `surge server status` | Prints running/not-running status from PID/port state. |

### Global Flags

These are persistent flags and can be used with all commands.

| Flag | Description |
| :--- | :--- |
| `--host <host:port>` | Target server for TUI and CLI actions. |
| `--token <token>` | Bearer token used for API requests. |
| `--verbose, -v` | Enable verbose logging. |

### Environment Variables

| Variable | Description |
| :--- | :--- |
| `SURGE_HOST` | Default host when `--host` is not provided. |
| `SURGE_TOKEN` | Default token when `--token` is not provided. |
