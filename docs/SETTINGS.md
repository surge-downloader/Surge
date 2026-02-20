# Settings & Configuration

This document covers all configuration options available in Surge. For CLI commands and flags, see [USAGE.md](USAGE.md).

## Configuration File

You can **access the settings in TUI** or if you prefer
from the `settings.json` file located in the application data directory:

- **Windows:** `%APPDATA%\surge\settings.json`
- **macOS:** `~/Library/Application Support/surge/settings.json`
- **Linux:** `~/.config/surge/settings.json`

## Directory Structure

Surge follows OS conventions for storing its files. Below is a breakdown of every directory it uses and where to find it on each platform.

| Directory   | Purpose                           | Linux                        | macOS                                       | Windows                 |
| :---------- | :-------------------------------- | :--------------------------- | :------------------------------------------ | :---------------------- |
| **Config**  | `settings.json`                   | `~/.config/surge/`           | `~/Library/Application Support/surge/`      | `%APPDATA%\surge\`      |
| **State**   | Database (`surge.db`), auth token | `~/.local/state/surge/`      | `~/Library/Application Support/surge/`      | `%APPDATA%\surge\`      |
| **Logs**    | Timestamped `.log` files          | `~/.local/state/surge/logs/` | `~/Library/Application Support/surge/logs/` | `%APPDATA%\surge\logs\` |
| **Runtime** | PID file, port file, lock         | `$XDG_RUNTIME_DIR/surge/`¹   | `$TMPDIR/surge-runtime/`                    | `%TEMP%\surge\`         |

> ¹ Falls back to `~/.local/state/surge/` when `$XDG_RUNTIME_DIR` is not set (e.g. Docker / headless).

> **Note:** On Linux, `$XDG_CONFIG_HOME` / `$XDG_STATE_HOME` are respected if set; the paths above show the defaults.

---

### General Settings

| Key                    | Type   | Description                                                                                        | Default |
| :--------------------- | :----- | :------------------------------------------------------------------------------------------------- | :------ |
| `default_download_dir` | string | Directory where new downloads are saved. If empty, defaults to `~/Downloads` or current directory. | `""`    |
| `warn_on_duplicate`    | bool   | Show a warning when adding a download that already exists in the list.                             | `true`  |
| `extension_prompt`     | bool   | Prompt for confirmation in the TUI when adding downloads via the browser extension.                | `false` |
| `auto_resume`          | bool   | Automatically resume paused downloads when Surge starts.                                           | `false` |
| `skip_update_check`    | bool   | Disable automatic check for new versions on startup.                                               | `false` |
| `clipboard_monitor`    | bool   | Watch the system clipboard for URLs and prompt to download them.                                   | `true`  |
| `theme`                | int    | UI Theme (0=Adaptive, 1=Light, 2=Dark).                                                            | `0`     |
| `log_retention_count`  | int    | Number of recent log files to keep.                                                                | `5`     |

### Connection Settings

| Key                        | Type   | Description                                                                                           | Default |
| :------------------------- | :----- | :---------------------------------------------------------------------------------------------------- | :------ |
| `max_connections_per_host` | int    | Maximum concurrent connections allowed to a single host (1-64).                                       | `32`    |
| `max_concurrent_downloads` | int    | Maximum number of downloads running simultaneously (requires restart).                                | `3`     |
| `user_agent`               | string | Custom User-Agent string for HTTP requests. Leave empty for default.                                  | `""`    |
| `proxy_url`                | string | HTTP/HTTPS proxy URL (e.g., `http://127.0.0.1:8080`). Leave empty to use system settings.             | `""`    |
| `sequential_download`      | bool   | Download file pieces in strict order (Streaming Mode). Useful for previewing media but may be slower. | `false` |
| `min_chunk_size`           | int64  | Minimum size of a download chunk in bytes (e.g., `2097152` for 2MB).                                  | `2MB`   |
| `worker_buffer_size`       | int    | I/O buffer size per worker in bytes (e.g., `524288` for 512KB).                                       | `512KB` |

### Performance Settings

| Key                        | Type     | Description                                                                  | Default |
| :------------------------- | :------- | :--------------------------------------------------------------------------- | :------ |
| `max_task_retries`         | int      | Number of times to retry a failed chunk before giving up.                    | `3`     |
| `slow_worker_threshold`    | float    | Restart workers slower than this fraction of the mean speed (0.0-1.0).       | `0.3`   |
| `slow_worker_grace_period` | duration | Time to wait before checking a worker's speed (e.g., `5s`).                  | `5s`    |
| `stall_timeout`            | duration | Restart workers that haven't received data for this duration (e.g., `3s`).   | `3s`    |
| `speed_ema_alpha`          | float    | Exponential moving average smoothing factor for speed calculation (0.0-1.0). | `0.3`   |
