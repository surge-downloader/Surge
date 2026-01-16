package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap defines the keybindings for the entire application
type KeyMap struct {
	Dashboard      DashboardKeyMap
	Input          InputKeyMap
	FilePicker     FilePickerKeyMap
	History        HistoryKeyMap
	Duplicate      DuplicateKeyMap
	Extension      ExtensionKeyMap
	Settings       SettingsKeyMap
	SettingsEditor SettingsEditorKeyMap
	BatchConfirm   BatchConfirmKeyMap
}

// DashboardKeyMap defines keybindings for the main dashboard
type DashboardKeyMap struct {
	TabQueued   key.Binding
	TabActive   key.Binding
	TabDone     key.Binding
	NextTab     key.Binding
	Add         key.Binding
	BatchImport key.Binding
	Search      key.Binding
	Pause       key.Binding
	Delete      key.Binding
	Settings    key.Binding
	Log         key.Binding
	History     key.Binding
	Quit        key.Binding
	ForceQuit   key.Binding
	// Navigation
	Up   key.Binding
	Down key.Binding
	// Log Navigation
	LogUp     key.Binding
	LogDown   key.Binding
	LogTop    key.Binding
	LogBottom key.Binding
	LogClose  key.Binding
}

// InputKeyMap defines keybindings for the add download input
type InputKeyMap struct {
	Tab    key.Binding
	Enter  key.Binding
	Esc    key.Binding
	Up     key.Binding
	Down   key.Binding
	Cancel key.Binding
}

// FilePickerKeyMap defines keybindings for the file picker
type FilePickerKeyMap struct {
	UseDir   key.Binding
	GotoHome key.Binding
	Back     key.Binding
	Forward  key.Binding
	Open     key.Binding
	Cancel   key.Binding
}

// HistoryKeyMap defines keybindings for the history view
type HistoryKeyMap struct {
	Up     key.Binding
	Down   key.Binding
	Delete key.Binding
	Close  key.Binding
}

// DuplicateKeyMap defines keybindings for duplicate warning
type DuplicateKeyMap struct {
	Continue key.Binding
	Focus    key.Binding
	Cancel   key.Binding
}

// ExtensionKeyMap defines keybindings for extension confirmation
type ExtensionKeyMap struct {
	Yes    key.Binding
	No     key.Binding
	Cancel key.Binding
}

// SettingsKeyMap defines keybindings for the settings view
type SettingsKeyMap struct {
	Tab1    key.Binding
	Tab2    key.Binding
	Tab3    key.Binding
	Tab4    key.Binding
	NextTab key.Binding
	PrevTab key.Binding
	Browse  key.Binding
	Edit    key.Binding
	Up      key.Binding
	Down    key.Binding
	Reset   key.Binding
	Close   key.Binding
}

// SettingsEditorKeyMap defines keybindings for editing a setting
type SettingsEditorKeyMap struct {
	Confirm key.Binding
	Cancel  key.Binding
}

// BatchConfirmKeyMap defines keybindings for batch import confirmation
type BatchConfirmKeyMap struct {
	Confirm key.Binding
	Cancel  key.Binding
}

// Keys contains all the keybindings for the application
var Keys = KeyMap{
	Dashboard: DashboardKeyMap{
		TabQueued: key.NewBinding(
			key.WithKeys("q"),
			key.WithHelp("q", "queued tab"),
		),
		TabActive: key.NewBinding(
			key.WithKeys("w"),
			key.WithHelp("w", "active tab"),
		),
		TabDone: key.NewBinding(
			key.WithKeys("e"),
			key.WithHelp("e", "done tab"),
		),
		NextTab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "next tab"),
		),
		Add: key.NewBinding(
			key.WithKeys("a"),
			key.WithHelp("a", "add download"),
		),
		BatchImport: key.NewBinding(
			key.WithKeys("b", "B"),
			key.WithHelp("b", "batch import"),
		),
		Search: key.NewBinding(
			key.WithKeys("f"),
			key.WithHelp("f", "search"),
		),
		Pause: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "pause/resume"),
		),
		Delete: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "delete"),
		),
		Settings: key.NewBinding(
			key.WithKeys("s"),
			key.WithHelp("s", "settings"),
		),
		Log: key.NewBinding(
			key.WithKeys("l"),
			key.WithHelp("l", "toggle log"),
		),
		History: key.NewBinding(
			key.WithKeys("h"),
			key.WithHelp("h", "history"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c", "ctrl+q"),
			key.WithHelp("ctrl+q", "quit"),
		),
		ForceQuit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "force quit"),
		),
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		LogUp: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "scroll up"),
		),
		LogDown: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "scroll down"),
		),
		LogTop: key.NewBinding(
			key.WithKeys("g"),
			key.WithHelp("g", "top"),
		),
		LogBottom: key.NewBinding(
			key.WithKeys("G"),
			key.WithHelp("G", "bottom"),
		),
		LogClose: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "close log"),
		),
	},
	Input: InputKeyMap{
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "browse/next"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "confirm/next"),
		),
		Esc: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
		Up: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "previous"),
		),
		Down: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "next"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
	},
	FilePicker: FilePickerKeyMap{
		UseDir: key.NewBinding(
			key.WithKeys("."),
			key.WithHelp(".", "use current"),
		),
		GotoHome: key.NewBinding(
			key.WithKeys("h", "H"),
			key.WithHelp("h", "home"),
		),
		Back: key.NewBinding(
			key.WithKeys("left"),
			key.WithHelp("←", "back"),
		),
		Forward: key.NewBinding(
			key.WithKeys("right"),
			key.WithHelp("→", "open"),
		),
		Open: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "select"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
	},
	History: HistoryKeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		Delete: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "remove"),
		),
		Close: key.NewBinding(
			key.WithKeys("esc", "q"),
			key.WithHelp("esc", "close"),
		),
	},
	Duplicate: DuplicateKeyMap{
		Continue: key.NewBinding(
			key.WithKeys("c", "C"),
			key.WithHelp("c", "continue"),
		),
		Focus: key.NewBinding(
			key.WithKeys("f", "F"),
			key.WithHelp("f", "focus existing"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("x", "X", "esc"),
			key.WithHelp("x", "cancel"),
		),
	},
	Extension: ExtensionKeyMap{
		Yes: key.NewBinding(
			key.WithKeys("y", "Y"),
			key.WithHelp("y", "yes"),
		),
		No: key.NewBinding(
			key.WithKeys("n", "N", "esc"),
			key.WithHelp("n", "no"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
	},
	Settings: SettingsKeyMap{
		NextTab: key.NewBinding(
			key.WithKeys("right"),
			key.WithHelp("→", "next tab"),
		),
		PrevTab: key.NewBinding(
			key.WithKeys("left"),
			key.WithHelp("←", "prev tab"),
		),
		Browse: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "browse dir"),
		),
		Edit: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "edit"),
		),
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "down"),
		),
		Reset: key.NewBinding(
			key.WithKeys("r", "R"),
			key.WithHelp("r", "reset"),
		),
		Close: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "save & close"),
		),
	},
	SettingsEditor: SettingsEditorKeyMap{
		Confirm: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "confirm"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
	},
	BatchConfirm: BatchConfirmKeyMap{
		Confirm: key.NewBinding(
			key.WithKeys("y", "Y", "enter"),
			key.WithHelp("y", "confirm"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("n", "N", "esc"),
			key.WithHelp("n", "cancel"),
		),
	},
}

// ShortHelp returns keybindings to show in the mini help view
func (k DashboardKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.TabQueued, k.TabActive, k.TabDone, k.Add, k.BatchImport, k.Search, k.Pause, k.Delete, k.Settings, k.Quit}
}

// FullHelp returns keybindings for the expanded help view
func (k DashboardKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.TabQueued, k.TabActive, k.TabDone, k.NextTab},
		{k.Add, k.Search, k.Pause, k.Delete, k.Settings},
		{k.Log, k.History, k.Quit},
	}
}

func (k InputKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Tab, k.Enter, k.Esc}
}

func (k InputKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Tab, k.Enter, k.Esc}}
}

func (k FilePickerKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Back, k.Forward, k.UseDir, k.GotoHome, k.Open, k.Cancel}
}

func (k FilePickerKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Back, k.Forward, k.UseDir, k.GotoHome, k.Open, k.Cancel}}
}

func (k HistoryKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Delete, k.Close}
}

func (k HistoryKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Up, k.Down, k.Delete, k.Close}}
}

func (k DuplicateKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Continue, k.Focus, k.Cancel}
}

func (k DuplicateKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Continue, k.Focus, k.Cancel}}
}

func (k ExtensionKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Yes, k.No}
}

func (k ExtensionKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Yes, k.No}}
}

func (k SettingsKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.PrevTab, k.NextTab, k.Edit, k.Reset, k.Close}
}

func (k SettingsKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Tab1, k.Tab2, k.Tab3, k.Tab4},
		{k.PrevTab, k.NextTab, k.Up, k.Down, k.Edit, k.Reset, k.Browse, k.Close},
	}
}

func (k SettingsEditorKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Confirm, k.Cancel}
}

func (k SettingsEditorKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Confirm, k.Cancel}}
}

func (k BatchConfirmKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Confirm, k.Cancel}
}

func (k BatchConfirmKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Confirm, k.Cancel}}
}
