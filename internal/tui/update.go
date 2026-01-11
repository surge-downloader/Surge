package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"surge/internal/downloader"
	"surge/internal/messages"
	"surge/internal/utils"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
)

// notificationTickMsg is sent to check if a notification should be cleared
type notificationTickMsg struct{}

// notificationTickCmd waits briefly then sends a tick to check notification expiry
func notificationTickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg {
		return notificationTickMsg{}
	})
}

// addLogEntry adds a log entry to the log viewport
func (m *RootModel) addLogEntry(msg string) {
	timestamp := time.Now().Format("15:04:05")
	entry := fmt.Sprintf("[%s] %s", timestamp, msg)
	m.logEntries = append(m.logEntries, entry)

	// Keep only the last 100 entries to prevent memory issues
	if len(m.logEntries) > 100 {
		m.logEntries = m.logEntries[len(m.logEntries)-100:]
	}

	// Update viewport content
	m.logViewport.SetContent(strings.Join(m.logEntries, "\n"))
	// Auto-scroll to bottom
	m.logViewport.GotoBottom()
}

// Update handles messages and updates the model
func (m RootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case StartDownloadMsg:
		// Handle download request from HTTP server
		path := msg.Path
		if path == "" {
			path = "."
		}

		// Check for duplicate URL in active downloads
		for _, d := range m.downloads {
			if d.URL == msg.URL {
				m.pendingURL = msg.URL
				m.pendingPath = path
				m.pendingFilename = msg.Filename
				m.duplicateInfo = d.Filename
				m.state = DuplicateWarningState
				return m, nil
			}
		}

		nextID := m.NextDownloadID
		m.NextDownloadID++
		newDownload := NewDownloadModel(nextID, msg.URL, "Queued", 0)
		m.downloads = append(m.downloads, newDownload)

		cfg := downloader.DownloadConfig{
			URL:        msg.URL,
			OutputPath: path,
			ID:         nextID,
			Filename:   msg.Filename,
			Verbose:    false,
			ProgressCh: m.progressChan,
			State:      newDownload.state,
		}

		utils.Debug("Adding download from server: %s", msg.URL)
		m.Pool.Add(cfg)

		// Update list items
		m.UpdateListItems()
		return m, nil

	case messages.DownloadStartedMsg:
		// Find the download and update with real metadata + start polling
		for _, d := range m.downloads {
			if d.ID == msg.DownloadID {
				d.Filename = msg.Filename
				d.Total = msg.Total
				d.URL = msg.URL
				// Update the progress state with real total size
				d.state.SetTotalSize(msg.Total)
				// Start polling for this download
				cmds = append(cmds, d.reporter.PollCmd())
				break
			}
		}
		// Update list items to reflect new filename
		m.UpdateListItems()
		// Add log entry
		m.addLogEntry(LogStyleStarted.Render("⬇ Started: " + msg.Filename))
		cmds = append(cmds, listenForActivity(m.progressChan))

	case messages.ProgressMsg:
		// Progress from polling reporter
		for _, d := range m.downloads {
			if d.ID == msg.DownloadID {
				// Don't update if already done or paused
				if d.done || d.paused {
					break
				}

				d.Downloaded = msg.Downloaded
				d.Speed = msg.Speed
				d.Elapsed = time.Since(d.StartTime)
				d.Connections = msg.ActiveConnections

				if d.Total > 0 {
					percentage := float64(d.Downloaded) / float64(d.Total)
					cmd := d.progress.SetPercent(percentage)
					cmds = append(cmds, cmd)
				}
				// Continue polling only if not done and not paused
				if !d.done && !d.paused {
					cmds = append(cmds, d.reporter.PollCmd())
				}

				// Add current speed to buffer for rolling average
				totalSpeed := m.calcTotalSpeed()
				m.speedBuffer = append(m.speedBuffer, totalSpeed)
				// Keep only last 10 samples (10 polls × 150ms = 1.5s window)
				if len(m.speedBuffer) > 10 {
					m.speedBuffer = m.speedBuffer[1:]
				}

				// Update global speed history every 500ms with rolling average
				if time.Since(m.lastSpeedHistoryUpdate) >= 500*time.Millisecond {
					// Calculate average of buffer
					var avgSpeed float64
					if len(m.speedBuffer) > 0 {
						for _, s := range m.speedBuffer {
							avgSpeed += s
						}
						avgSpeed /= float64(len(m.speedBuffer))
					}
					if len(m.SpeedHistory) > 0 {
						m.SpeedHistory = append(m.SpeedHistory[1:], avgSpeed)
					}
					m.lastSpeedHistoryUpdate = time.Now()
				}

				// Update list to show current progress
				m.UpdateListItems()
				break
			}
		}

	case messages.DownloadCompleteMsg:
		for _, d := range m.downloads {
			if d.ID == msg.DownloadID {
				if d.done {
					break
				}
				d.Downloaded = d.Total
				d.Elapsed = msg.Elapsed
				d.done = true
				// Set progress to 100%
				cmds = append(cmds, d.progress.SetPercent(1.0))

				// Add log entry
				speed := float64(d.Total) / msg.Elapsed.Seconds()
				m.addLogEntry(LogStyleComplete.Render(fmt.Sprintf("✔ Done: %s (%.2f MB/s)", d.Filename, speed/Megabyte)))

				// Persist to history (TUI has the correct filename from DownloadStartedMsg)
				_ = downloader.AddToMasterList(downloader.DownloadEntry{
					URLHash:     downloader.URLHash(d.URL),
					URL:         d.URL,
					DestPath:    "", // Not tracked in TUI model
					Filename:    d.Filename,
					Status:      "completed",
					TotalSize:   d.Total,
					CompletedAt: time.Now().Unix(),
				})

				break
			}
		}
		// Update list items
		m.UpdateListItems()
		cmds = append(cmds, listenForActivity(m.progressChan))

	case messages.DownloadErrorMsg:
		for _, d := range m.downloads {
			if d.ID == msg.DownloadID {
				d.err = msg.Err
				d.done = true
				// Add log entry
				m.addLogEntry(LogStyleError.Render("✖ Error: " + d.Filename))
				break
			}
		}
		m.UpdateListItems()
		cmds = append(cmds, listenForActivity(m.progressChan))

	case messages.DownloadPausedMsg:
		for _, d := range m.downloads {
			if d.ID == msg.DownloadID {
				d.paused = true
				d.Downloaded = msg.Downloaded
				d.Speed = 0 // Clear speed when paused
				// Add log entry
				m.addLogEntry(LogStylePaused.Render("⏸ Paused: " + d.Filename))
				break
			}
		}
		m.UpdateListItems()
		cmds = append(cmds, listenForActivity(m.progressChan))

	case messages.DownloadResumedMsg:
		for _, d := range m.downloads {
			if d.ID == msg.DownloadId {
				d.paused = false
				// Add log entry
				m.addLogEntry(LogStyleStarted.Render("▶ Resumed: " + d.Filename))
				// Restart polling
				cmds = append(cmds, d.reporter.PollCmd())
				break
			}
		}
		m.UpdateListItems()
		cmds = append(cmds, listenForActivity(m.progressChan))

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		// Calculate list dimensions
		// List goes in bottom-left pane
		availableWidth := msg.Width - 4
		leftWidth := int(float64(availableWidth) * ListWidthRatio)

		// Calculate list height (total height - header row - margins)
		topHeight := 9
		bottomHeight := msg.Height - topHeight - 5
		if bottomHeight < 10 {
			bottomHeight = 10
		}

		m.list.SetSize(leftWidth-2, bottomHeight-4)

		// Update list title based on active tab
		m.updateListTitle()
		m.UpdateListItems()
		return m, nil

	case notificationTickMsg:
		// Notification tick is still used but logs don't expire
		return m, nil

	// Handle filepicker messages for all message types when in FilePickerState
	default:
		if m.state == FilePickerState {
			var cmd tea.Cmd
			m.filepicker, cmd = m.filepicker.Update(msg)

			// Check if a directory was selected
			if didSelect, path := m.filepicker.DidSelectFile(msg); didSelect {
				m.inputs[1].SetValue(path)
				m.state = InputState
				return m, nil
			}

			return m, cmd
		}

	case tea.KeyMsg:
		switch m.state {
		case DashboardState:
			// Tab switching with Q/W/E
			if msg.String() == "q" {
				m.activeTab = TabQueued
				m.updateListTitle()
				m.UpdateListItems()
				return m, nil
			}
			if msg.String() == "w" {
				m.activeTab = TabActive
				m.updateListTitle()
				m.UpdateListItems()
				return m, nil
			}
			if msg.String() == "e" {
				m.activeTab = TabDone
				m.updateListTitle()
				m.UpdateListItems()
				return m, nil
			}
			// Ctrl+Q to quit
			if msg.String() == "ctrl+q" || msg.String() == "ctrl+c" {
				// Graceful shutdown: pause all active downloads to save state
				m.Pool.PauseAll()
				return m, tea.Quit
			}
			if msg.String() == "a" {
				m.state = InputState
				m.focusedInput = 0
				m.inputs[0].SetValue("")
				m.inputs[0].Focus()
				m.inputs[1].SetValue(".")
				m.inputs[1].Blur()
				m.inputs[2].SetValue("")
				m.inputs[2].Blur()
				return m, nil
			}
			if msg.String() == "tab" {
				m.activeTab = (m.activeTab + 1) % 3
				m.updateListTitle()
				m.UpdateListItems()
				return m, nil
			}
			// Delete download with D or X
			if msg.String() == "d" || msg.String() == "x" {
				// Don't process delete if list is filtering
				if m.list.FilterState() == list.Filtering {
					// Fall through to let list handle it
				} else if d := m.GetSelectedDownload(); d != nil {
					targetID := d.ID

					// Find index in real list
					realIdx := -1
					for i, dl := range m.downloads {
						if dl.ID == targetID {
							realIdx = i
							break
						}
					}

					if realIdx != -1 {
						dl := m.downloads[realIdx]

						// Cancel if active
						m.Pool.Cancel(dl.ID)

						// Delete state files
						if dl.URL != "" {
							_ = downloader.DeleteStateByURL(dl.URL)
						}

						// Remove from list
						m.downloads = append(m.downloads[:realIdx], m.downloads[realIdx+1:]...)
					}
					m.UpdateListItems()
					return m, nil
				}
			}
			if msg.String() == "h" {
				// Open history view
				if entries, err := downloader.LoadCompletedDownloads(); err == nil {
					m.historyEntries = entries
					m.historyCursor = 0
					m.state = HistoryState
				}
				return m, nil
			}

			// Pause/Resume toggle - get selected download from list
			if msg.String() == "p" {
				if d := m.GetSelectedDownload(); d != nil {
					if !d.done {
						if d.paused {
							// Resume: create config and add to pool
							d.paused = false
							d.state.Resume()
							cfg := downloader.DownloadConfig{
								URL:        d.URL,
								OutputPath: m.PWD, // Will be resolved in TUIDownload
								ID:         d.ID,
								Filename:   d.Filename,
								Verbose:    false,
								ProgressCh: m.progressChan,
								State:      d.state,
							}
							m.Pool.Add(cfg)
							// Restart polling
							cmds = append(cmds, d.reporter.PollCmd())
						} else {
							m.Pool.Pause(d.ID)
						}
					}
				}
				m.UpdateListItems()
				return m, tea.Batch(cmds...)
			}

			// Toggle log focus with L
			if msg.String() == "l" {
				m.logFocused = !m.logFocused
				return m, nil
			}

			// If log is focused, handle viewport scrolling
			if m.logFocused {
				switch msg.String() {
				case "esc":
					m.logFocused = false
					return m, nil
				case "j", "down":
					m.logViewport.LineDown(1)
					return m, nil
				case "k", "up":
					m.logViewport.LineUp(1)
					return m, nil
				case "g":
					m.logViewport.GotoTop()
					return m, nil
				case "G":
					m.logViewport.GotoBottom()
					return m, nil
				}
				return m, nil
			}

			// Pass messages to the list for navigation/filtering
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			cmds = append(cmds, cmd)
			return m, tea.Batch(cmds...)

		case DetailState:
			if msg.String() == "esc" || msg.String() == "q" || msg.String() == "enter" {
				m.state = DashboardState
				return m, nil
			}

		case InputState:
			if msg.String() == "esc" {
				m.state = DashboardState
				return m, nil
			}
			// Tab to open file picker when on path input
			if msg.String() == "tab" && m.focusedInput == 1 {
				m.state = FilePickerState
				// Reset filepicker to current directory
				m.filepicker.CurrentDirectory = m.PWD
				return m, m.filepicker.Init()
			}
			if msg.String() == "enter" {
				// Navigate through inputs: URL -> Path -> Filename -> Start
				if m.focusedInput < 2 {
					m.inputs[m.focusedInput].Blur()
					m.focusedInput++
					m.inputs[m.focusedInput].Focus()
					return m, nil
				}
				// Start download (on last input)
				url := m.inputs[0].Value()
				if url == "" {
					// URL is mandatory - don't start
					m.focusedInput = 0
					m.inputs[0].Focus()
					m.inputs[1].Blur()
					m.inputs[2].Blur()
					return m, nil
				}
				path := m.inputs[1].Value()
				if path == "" {
					path = "."
				}
				filename := m.inputs[2].Value()

				// Check for duplicate URL in active downloads
				for _, d := range m.downloads {
					if d.URL == url {
						m.pendingURL = url
						m.pendingPath = path
						m.pendingFilename = filename
						m.duplicateInfo = d.Filename
						m.state = DuplicateWarningState
						return m, nil
					}
				}

				m.state = DashboardState

				// Create download with state and reporter
				nextID := m.NextDownloadID
				m.NextDownloadID++
				newDownload := NewDownloadModel(nextID, url, "Queued", 0)
				m.downloads = append(m.downloads, newDownload)

				// Create config
				cfg := downloader.DownloadConfig{
					URL:        url,
					OutputPath: path,
					ID:         nextID,
					Verbose:    false,
					ProgressCh: m.progressChan,
					State:      newDownload.state,
				}

				utils.Debug("Adding to Queue")
				m.Pool.Add(cfg)

				m.UpdateListItems()
				return m, nil
			}

			// Up/Down navigation between inputs
			if msg.String() == "up" && m.focusedInput > 0 {
				m.inputs[m.focusedInput].Blur()
				m.focusedInput--
				m.inputs[m.focusedInput].Focus()
				return m, nil
			}
			if msg.String() == "down" && m.focusedInput < 2 {
				m.inputs[m.focusedInput].Blur()
				m.focusedInput++
				m.inputs[m.focusedInput].Focus()
				return m, nil
			}

			var cmd tea.Cmd
			m.inputs[m.focusedInput], cmd = m.inputs[m.focusedInput].Update(msg)
			return m, cmd

		case FilePickerState:
			if msg.String() == "esc" {
				// Cancel and return to input state
				m.state = InputState
				return m, nil
			}

			// H key to jump to Downloads folder
			if msg.String() == "h" || msg.String() == "H" {
				homeDir, _ := os.UserHomeDir()
				m.filepicker.CurrentDirectory = filepath.Join(homeDir, "Downloads")
				return m, m.filepicker.Init()
			}

			// '.' to select current directory
			if msg.String() == "." {
				m.inputs[1].SetValue(m.filepicker.CurrentDirectory)
				m.state = InputState
				return m, nil
			}

			// Pass key to filepicker
			var cmd tea.Cmd
			m.filepicker, cmd = m.filepicker.Update(msg)

			// Check if a directory was selected
			if didSelect, path := m.filepicker.DidSelectFile(msg); didSelect {
				// Set the path input value and return to input state
				m.inputs[1].SetValue(path)
				m.state = InputState
				return m, nil
			}

			return m, cmd

		case HistoryState:
			if msg.String() == "esc" || msg.String() == "q" {
				m.state = DashboardState
				return m, nil
			}
			if msg.String() == "up" || msg.String() == "k" {
				if m.historyCursor > 0 {
					m.historyCursor--
				}
				return m, nil
			}
			if msg.String() == "down" || msg.String() == "j" {
				if m.historyCursor < len(m.historyEntries)-1 {
					m.historyCursor++
				}
				return m, nil
			}
			if msg.String() == "d" || msg.String() == "x" {
				if m.historyCursor >= 0 && m.historyCursor < len(m.historyEntries) {
					entry := m.historyEntries[m.historyCursor]
					_ = downloader.RemoveFromMasterList(entry.URLHash)
					m.historyEntries, _ = downloader.LoadCompletedDownloads()
					if m.historyCursor >= len(m.historyEntries) && m.historyCursor > 0 {
						m.historyCursor--
					}
				}
				return m, nil
			}
			return m, nil

		case DuplicateWarningState:
			if msg.String() == "c" || msg.String() == "C" {
				// Continue anyway - add the download
				nextID := m.NextDownloadID
				m.NextDownloadID++
				newDownload := NewDownloadModel(nextID, m.pendingURL, "Queued", 0)
				m.downloads = append(m.downloads, newDownload)

				cfg := downloader.DownloadConfig{
					URL:        m.pendingURL,
					OutputPath: m.pendingPath,
					ID:         nextID,
					Filename:   m.pendingFilename,
					Verbose:    false,
					ProgressCh: m.progressChan,
					State:      newDownload.state,
				}
				m.Pool.Add(cfg)
				m.state = DashboardState
				m.UpdateListItems()
				return m, nil
			}
			if msg.String() == "x" || msg.String() == "X" || msg.String() == "esc" {
				// Cancel - don't add
				m.state = DashboardState
				return m, nil
			}
			if msg.String() == "f" || msg.String() == "F" {
				// Focus existing download - find it and select in list
				for i, d := range m.getFilteredDownloads() {
					if d.URL == m.pendingURL {
						m.list.Select(i)
						break
					}
				}
				m.state = DashboardState
				return m, nil
			}
			return m, nil
		}
	}

	// Propagate messages to progress bars - only update visible ones for performance
	for _, d := range m.downloads {
		var cmd tea.Cmd
		var newModel tea.Model
		newModel, cmd = d.progress.Update(msg)
		if p, ok := newModel.(progress.Model); ok {
			d.progress = p
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	return m, tea.Batch(cmds...)
}

// updateListTitle updates the list title based on active tab
func (m *RootModel) updateListTitle() {
	switch m.activeTab {
	case TabQueued:
		m.list.Title = "📋 Queued"
	case TabActive:
		m.list.Title = "⬇️ Active"
	case TabDone:
		m.list.Title = "✅ Completed"
	}
}
