package tui

import (
	"os"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"surge/internal/downloader"
)

type UIState int //Defines UIState as int to be used in rootModel

const (
	DashboardState UIState = iota //DashboardState is 0 increments after each line
	InputState                    //InputState is 1
	DetailState                   //DetailState is 2
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
	inputs         []textinput.Model
	focusedInput   int
	progressChan   chan tea.Msg // Channel for events only (start/complete/error)

	// Navigation
	cursor int

	Pool *downloader.WorkerPool //Works as the download queue
	PWD  string
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

	// Load paused downloads from master list
	var downloads []*DownloadModel
	surgeDir := pwd + "/.surge"
	if pausedEntries, err := downloader.LoadPausedDownloads(surgeDir); err == nil {
		for i, entry := range pausedEntries {
			id := i + 1 // Assign sequential IDs
			dm := NewDownloadModel(id, entry.URL, entry.Filename, 0)
			dm.paused = true
			// Load actual progress from state file
			if state, err := downloader.LoadState(entry.DestPath, entry.URL); err == nil {
				dm.Downloaded = state.Downloaded
				dm.Total = state.TotalSize
				dm.state.Downloaded.Store(state.Downloaded)
				dm.state.SetTotalSize(state.TotalSize)
			}
			downloads = append(downloads, dm)
		}
	}

	return RootModel{
		downloads:      downloads,
		NextDownloadID: len(downloads) + 1, // Start after loaded downloads
		inputs:         []textinput.Model{urlInput, pathInput, filenameInput},
		state:          DashboardState,
		progressChan:   progressChan,
		Pool:           downloader.NewWorkerPool(progressChan),
		PWD:            pwd,
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
