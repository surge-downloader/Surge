package tui

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/surge-downloader/surge/internal/config"
	"github.com/surge-downloader/surge/internal/core"
	"github.com/surge-downloader/surge/internal/engine/types"
	"github.com/surge-downloader/surge/internal/version"
)

type UIState int // Defines UIState as int to be used in rootModel

const (
	DashboardState             UIState = iota // DashboardState is 0 increments after each line
	InputState                                // InputState is 1
	DetailState                               // DetailState is 2
	FilePickerState                           // FilePickerState is 3
	HistoryState                              // HistoryState is 4
	DuplicateWarningState                     // DuplicateWarningState is 5
	SearchState                               // SearchState is 6
	SettingsState                             // SettingsState is 7
	ExtensionConfirmationState                // ExtensionConfirmationState is 8
	BatchFilePickerState                      // BatchFilePickerState is 9
	BatchConfirmState                         // BatchConfirmState is 10
	UpdateAvailableState                      // UpdateAvailableState is 11
)

const (
	TabQueued = 0
	TabActive = 1
	TabDone   = 2
)

type DownloadModel struct {
	ID          string
	URL         string
	Filename    string
	Destination string // Full path to the destination file
	Total       int64
	Downloaded  int64
	Speed       float64
	Connections int

	StartTime time.Time
	Elapsed   time.Duration

	progress progress.Model

	// Unified architecture: View Model updated by events
	// No direct state access or polling reporter
	state *types.ProgressState // Keep for now if needed for details view, but mostly passive

	done          bool
	err           error
	paused        bool
	pausing       bool // UI state: transitioning to pause
	pendingResume bool // UI state: waiting for async resume
}

type RootModel struct {
	downloads    []*DownloadModel
	width        int
	height       int
	state        UIState
	activeTab    int // 0=Queued, 1=Active, 2=Done
	inputs       []textinput.Model
	focusedInput int
	// Service Interface (replaces Pool)
	Service core.DownloadService

	// File picker for directory selection
	filepicker filepicker.Model

	// Bubbles help component
	help help.Model

	// Bubbles list component for download listing
	list list.Model

	PWD string

	// History view
	historyEntries []types.DownloadEntry
	historyCursor  int

	// Duplicate detection
	pendingURL      string   // URL pending confirmation
	pendingPath     string   // Path pending confirmation
	pendingFilename string   // Filename pending confirmation
	pendingMirrors  []string // Mirrors pending confirmation
	pendingHeaders  map[string]string
	duplicateInfo   string // Info about the duplicate

	// Graph Data
	SpeedHistory           []float64 // Stores the last ~60 ticks of speed data
	lastSpeedHistoryUpdate time.Time // Last time SpeedHistory was updated (for 0.5s sampling)
	speedBuffer            []float64 // Buffer for rolling average (last 10 speed readings)

	// Notification log system
	logViewport viewport.Model // Scrollable log viewport
	logEntries  []string       // Log entries for download events
	logFocused  bool           // Whether the log viewport is focused

	// Settings
	Settings             *config.Settings // Application settings
	SettingsActiveTab    int              // Active category tab (0-3)
	SettingsSelectedRow  int              // Selected setting within current tab
	SettingsIsEditing    bool             // Whether currently editing a value
	SettingsInput        textinput.Model  // Input for editing string/int values
	SettingsFileBrowsing bool             // Whether browsing for a directory

	// Selection persistence
	SelectedDownloadID string // ID of the currently selected download
	ManualTabSwitch    bool   // Whether the last tab switch was manual

	// Search functionality
	searchInput  textinput.Model // Text input for search
	searchActive bool            // Whether search mode is active
	searchQuery  string          // Current search query

	// Batch import
	pendingBatchURLs []string // URLs pending batch import
	batchFilePath    string   // Path to the batch file

	// Keybindings
	keys KeyMap

	// Server port for display
	ServerPort int

	// Update check
	UpdateInfo     *version.UpdateInfo // Update information (nil if no update available)
	CurrentVersion string              // Current version of Surge

	InitialDarkBackground bool // Captured at startup for "System" theme
}

// NewDownloadModel creates a new download model
func NewDownloadModel(id string, url string, filename string, total int64) *DownloadModel {
	// Create dummy state container for compatibility if needed
	state := types.NewProgressState(id, total)
	return &DownloadModel{
		ID:        id,
		URL:       url,
		Filename:  filename,
		Total:     total,
		StartTime: time.Now(),
		progress:  progress.New(progress.WithSpringOptions(0.5, 0.1)),
		state:     state,
	}
}

func InitialRootModel(serverPort int, currentVersion string, service core.DownloadService, noResume bool) RootModel {
	// Initialize inputs
	urlInput := textinput.New()
	urlInput.Placeholder = "https://example.com/file.zip"
	urlInput.Focus()
	urlInput.Width = InputWidth
	urlInput.Prompt = ""

	pathInput := textinput.New()
	pathInput.Placeholder = "."
	pathInput.Width = InputWidth
	pathInput.Prompt = ""
	pathInput.SetValue(".")

	filenameInput := textinput.New()
	filenameInput.Placeholder = "(auto-detect)"
	filenameInput.Width = InputWidth
	filenameInput.Prompt = ""

	mirrorsInput := textinput.New()
	mirrorsInput.Placeholder = "http://mirror1.com, http://mirror2.com"
	mirrorsInput.Width = InputWidth
	mirrorsInput.Prompt = ""

	pwd, _ := os.Getwd()

	// Initialize file picker for directory selection - default to Downloads folder
	homeDir, _ := os.UserHomeDir()
	downloadsDir := filepath.Join(homeDir, "Downloads")
	fp := filepicker.New()
	fp.CurrentDirectory = downloadsDir
	fp.DirAllowed = true
	fp.FileAllowed = false
	fp.ShowHidden = false
	fp.ShowSize = true
	fp.ShowPermissions = true
	fp.SetHeight(FilePickerHeight)

	// Load settings for auto resume
	settings, _ := config.LoadSettings()
	if settings == nil {
		settings = config.DefaultSettings()
	}

	// Override AutoResume if CLI flag provided
	if noResume {
		settings.General.AutoResume = false
	}

	// Load paused downloads from master list (now uses global config directory)
	var downloads []*DownloadModel
	// Note: With Service abstraction, we might want to let the Service handle loading.
	// But LocalDownloadService's List() calls state.ListAllDownloads().
	// For TUI initialization, we should probably call Service.List() to populate the model.
	// However, Service.List() returns []DownloadStatus, which we need to convert to []*DownloadModel.

	// Let's use service.List() if available
	if service != nil {
		statuses, err := service.List()
		if err == nil {
			for _, s := range statuses {
				dm := NewDownloadModel(s.ID, s.URL, s.Filename, s.TotalSize)
				dm.Downloaded = s.Downloaded
				if s.DestPath != "" {
					dm.Destination = s.DestPath
				} else {
					dm.Destination = s.Filename // Fallback
				}
				// Status mapping
				switch s.Status {
				case "completed":
					dm.done = true
					dm.progress.SetPercent(1.0)
				case "paused":
					if settings.General.AutoResume {
						dm.pendingResume = true
						dm.paused = true // Will update when resume event received
					} else {
						dm.paused = true
					}
				case "queued":
					// Always resume queued items
					dm.pendingResume = true
					dm.paused = true // Will update when resume event received
				}

				if s.TotalSize > 0 {
					dm.progress.SetPercent(s.Progress / 100.0)
				}

				downloads = append(downloads, dm)
			}
		}
	}

	// Initialize the download list
	downloadList := NewDownloadList(80, 20) // Default size, will be resized on WindowSizeMsg

	// Initialize help
	helpModel := help.New()
	helpModel.Styles.ShortKey = lipgloss.NewStyle().Foreground(ColorLightGray)
	helpModel.Styles.ShortDesc = lipgloss.NewStyle().Foreground(ColorGray)

	// Initialize settings input for editing
	settingsInput := textinput.New()
	settingsInput.Width = 40
	settingsInput.Prompt = ""

	// Initialize search input
	searchInput := textinput.New()
	searchInput.Placeholder = "Type to search..."
	searchInput.Width = 30
	searchInput.Prompt = ""

	m := RootModel{
		downloads:             downloads,
		inputs:                []textinput.Model{urlInput, mirrorsInput, pathInput, filenameInput},
		state:                 DashboardState,
		filepicker:            fp,
		help:                  helpModel,
		list:                  downloadList,
		Service:               service,
		PWD:                   pwd,
		SpeedHistory:          make([]float64, GraphHistoryPoints), // 60 points of history (30s at 0.5s interval)
		logViewport:           viewport.New(40, 5),                 // Default size, will be resized
		logEntries:            make([]string, 0),
		Settings:              settings,
		SettingsInput:         settingsInput,
		searchInput:           searchInput,
		keys:                  Keys,
		ServerPort:            serverPort,
		CurrentVersion:        currentVersion,
		InitialDarkBackground: lipgloss.HasDarkBackground(),
	}

	// Apply configured theme
	// We can't call m.ApplyTheme yet as m is returned, so apply logic directly
	switch settings.General.Theme {
	case config.ThemeLight:
		lipgloss.SetHasDarkBackground(false)
	case config.ThemeDark:
		lipgloss.SetHasDarkBackground(true)
		// ThemeAdaptive: do nothing, already set by system detection
	}

	return m
}

func (m RootModel) Init() tea.Cmd {
	var cmds []tea.Cmd

	// Trigger update check if not disabled in settings
	if !m.Settings.General.SkipUpdateCheck {
		cmds = append(cmds, checkForUpdateCmd(m.CurrentVersion))
	}

	// Async resume of downloads
	for _, d := range m.downloads {
		if d.pendingResume {
			id := d.ID
			cmds = append(cmds, func() tea.Msg {
				err := m.Service.Resume(id)
				return resumeResultMsg{id: id, err: err}
			})
		}
	}

	return tea.Batch(cmds...)
}

type resumeResultMsg struct {
	id  string
	err error
}

// Helper to get downloads for the current tab
func (m RootModel) getFilteredDownloads() []*DownloadModel {
	var filtered []*DownloadModel
	searchLower := strings.ToLower(m.searchQuery)

	for _, d := range m.downloads {
		// Apply tab filter first
		switch m.activeTab {
		case TabQueued:
			if d.done || d.Speed > 0 {
				continue
			}
		case TabActive:
			if d.done || (d.Speed == 0 && d.Connections == 0) {
				continue
			}
		case TabDone:
			if !d.done {
				continue
			}
		}

		// Apply search filter if query is set
		if m.searchQuery != "" {
			if !strings.Contains(strings.ToLower(d.Filename), searchLower) {
				continue
			}
		}

		filtered = append(filtered, d)
	}
	return filtered
}

// newFilepicker creates a fresh filepicker instance with consistent settings.
// This is necessary to avoid cursor desync issues that cause "index out of range"
// panics when navigating directories (especially on Windows).
// See: https://github.com/charmbracelet/bubbles/issues/864
func newFilepicker(currentDir string) filepicker.Model {
	fp := filepicker.New()
	fp.CurrentDirectory = currentDir
	fp.DirAllowed = true
	fp.FileAllowed = false
	fp.ShowHidden = false
	fp.ShowSize = true
	fp.ShowPermissions = true
	fp.SetHeight(FilePickerHeight)
	return fp
}

// ApplyTheme applies the selected theme mode
func (m *RootModel) ApplyTheme(mode int) {
	switch mode {
	case config.ThemeAdaptive:
		// Restore initial system state
		lipgloss.SetHasDarkBackground(m.InitialDarkBackground)
	case config.ThemeLight:
		lipgloss.SetHasDarkBackground(false)
	case config.ThemeDark:
		lipgloss.SetHasDarkBackground(true)
	}
}
