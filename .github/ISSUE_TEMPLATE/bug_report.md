---
name: Bug report
about: Create a report to help us improve
title: ""
labels: bug
assignees: ""
---

**Describe the bug**
A clear and concise description of what the bug is.

**To Reproduce**
Steps to reproduce the behavior:

1. Go to '...'
2. Press '....'
3. Scroll down to '....'
4. See error/unexpected behaviour

**Expected behavior**
A clear and concise description of what you expected to happen.

**Screenshots**
If applicable, add screenshots to help explain your problem.

**Logs**
Surge only writes log files when run with `--verbose`. To capture a log:

1. Reproduce the bug with the `--verbose` flag:
   ```
   surge --verbose [your command]
   ```
2. The log file is written to:
   - **Linux:** `~/.local/state/surge/logs/`
   - **macOS:** `~/Library/Application Support/surge/logs/`
   - **Windows:** `%APPDATA%\surge\logs\`
3. Attach the most recent `debug-*.log` file by dragging it into this issue, or paste relevant excerpts in a code block.

**Please complete the following information:**

- OS: [e.g. Windows 11 / macOS 14 / Ubuntu 24.04]
- Surge Version: [e.g. 1.2.0 — run `surge --version`]
- Installed From: [e.g. Brew / GitHub Release / built from source]

**Additional context**
Add any other context about the problem here.
