package tui

import (
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"surge/internal/downloader"
)

type UIState int //Defines UIState as int to be used in rootModel

const (
	DashboardState        UIState = iota //DashboardState is 0 increments after each line
	InputState                           //InputState is 1
	DetailState                          //DetailState is 2
	FilePickerState                      //FilePickerState is 3
	HistoryState                         //HistoryState is 4
	DuplicateWarningState                //DuplicateWarningState is 5
	SearchState                          //SearchState is 6
)

const (
	TabQueued = 0
	TabActive = 1
	TabDone   = 2
)

// StartDownloadMsg is sent from the HTTP server to start a new download
type StartDownloadMsg struct {
	URL      string
	Path     string
	Filename string
}

type DownloadModel struct {
	ID          int
	URL         string
	Filename    string
	Total       int64
	Downloaded  int64
	Speed       float64
	Connections int

	StartTime time.Time
	Elapsed   time.Duration

	progress progress.Model

	// Hybrid architecture: atomic state + polling reporter
	state    *downloader.ProgressState
	reporter *ProgressReporter

	done   bool
	err    error
	paused bool
}

type RootModel struct {
	downloads      []*DownloadModel
	NextDownloadID int // Monotonic counter for unique download IDs
	width          int
	height         int
	state          UIState
	activeTab      int // 0=Queued, 1=Active, 2=Done
	inputs         []textinput.Model
	focusedInput   int
	progressChan   chan tea.Msg // Channel for events only (start/complete/error)

	// File picker for directory selection
	filepicker filepicker.Model

	// Bubbles help component
	help help.Model

	// Bubbles list component for download listing
	list list.Model

	Pool *downloader.WorkerPool //Works as the download queue
	PWD  string

	// History view
	historyEntries []downloader.DownloadEntry
	historyCursor  int

	// Duplicate detection
	pendingURL      string // URL pending confirmation
	pendingPath     string // Path pending confirmation
	pendingFilename string // Filename pending confirmation
	duplicateInfo   string // Info about the duplicate

	// Graph Data
	SpeedHistory           []float64 // Stores the last ~60 ticks of speed data
	lastSpeedHistoryUpdate time.Time // Last time SpeedHistory was updated (for 0.5s sampling)
	speedBuffer            []float64 // Buffer for rolling average (last 10 speed readings)

	// Notification log system
	logViewport viewport.Model // Scrollable log viewport
	logEntries  []string       // Log entries for download events
	logFocused  bool           // Whether the log viewport is focused
}

// NewDownloadModel creates a new download model with progress state and reporter
func NewDownloadModel(id int, url string, filename string, total int64) *DownloadModel {
	state := downloader.NewProgressState(id, total)
	return &DownloadModel{
		ID:        id,
		URL:       url,
		Filename:  filename,
		Total:     total,
		StartTime: time.Now(),
		progress:  progress.New(progress.WithDefaultGradient()),
		state:     state,
		reporter:  NewProgressReporter(state),
	}
}

func InitialRootModel() RootModel {
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

	// Create channel first so we can pass it to WorkerPool
	progressChan := make(chan tea.Msg, ProgressChannelBuffer)

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

	// Load paused downloads from master list (now uses global config directory)
	var downloads []*DownloadModel
	if pausedEntries, err := downloader.LoadPausedDownloads(); err == nil {
		for i, entry := range pausedEntries {
			id := i + 1 // Assign sequential IDs
			dm := NewDownloadModel(id, entry.URL, entry.Filename, 0)
			dm.paused = true
			// Load actual progress from state file
			if state, err := downloader.LoadState(entry.URL); err == nil {
				dm.Downloaded = state.Downloaded
				dm.Total = state.TotalSize
				dm.state.Downloaded.Store(state.Downloaded)
				dm.state.SetTotalSize(state.TotalSize)
				// Set progress bar to correct position
				if state.TotalSize > 0 {
					dm.progress.SetPercent(float64(state.Downloaded) / float64(state.TotalSize))
				}
			}
			downloads = append(downloads, dm)
		}
	}

	// Initialize the download list
	downloadList := NewDownloadList(80, 20) // Default size, will be resized on WindowSizeMsg

	// Initialize help
	helpModel := help.New()
	helpModel.Styles.ShortKey = lipgloss.NewStyle().Foreground(ColorLightGray)
	helpModel.Styles.ShortDesc = lipgloss.NewStyle().Foreground(ColorGray)

	return RootModel{
		downloads:      downloads,
		NextDownloadID: len(downloads) + 1, // Start after loaded downloads
		inputs:         []textinput.Model{urlInput, pathInput, filenameInput},
		state:          DashboardState,
		progressChan:   progressChan,
		filepicker:     fp,
		help:           helpModel,
		list:           downloadList,
		Pool:           downloader.NewWorkerPool(progressChan),
		PWD:            pwd,
		SpeedHistory:   make([]float64, 200), // 200 points of history to fill wide screens
		logViewport:    viewport.New(40, 5),  // Default size, will be resized
		logEntries:     make([]string, 0),
	}
}

func (m RootModel) Init() tea.Cmd {
	return listenForActivity(m.progressChan)
}

func listenForActivity(sub chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-sub
	}
}

// Helper to get downloads for the current tab
func (m RootModel) getFilteredDownloads() []*DownloadModel {
	var filtered []*DownloadModel
	for _, d := range m.downloads {
		switch m.activeTab {
		case TabQueued:
			// Queued: not done, not actively downloading (includes paused)
			if !d.done && d.Speed == 0 {
				filtered = append(filtered, d)
			}
		case TabActive:
			// Active: only downloads with active speed
			if !d.done && d.Speed > 0 {
				filtered = append(filtered, d)
			}
		case TabDone:
			if d.done {
				filtered = append(filtered, d)
			}
		}
	}
	return filtered
}
