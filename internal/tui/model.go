package tui

import (
	"os"

	"bcdl/internal"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"

	tea "github.com/charmbracelet/bubbletea"
)

type modelState uint

// Configure the different states for the model
const (
	showUsernameState modelState = iota
	showIdentityState
	showDirectoryPickerState
	showFormatListState
	showFilterState
)

// Outputs stores all of the user's input values
// There is one for each state
type Outputs struct {
	Username  string
	Identity  string
	Directory string
	FileType  internal.FileType
	Filter    string
}

var selected Outputs = Outputs{}

// model tracks each piece of the tui
type model struct {
	state modelState

	username  textinput.Model
	identity  textinput.Model
	directory filepicker.Model
	fileType  list.Model
	filter    textinput.Model
	help      help.Model

	keys KeyMap
	err  error
}

// Remap filepicker keys to better work with our program
func directoryPickerKeyMap() filepicker.KeyMap {
	return filepicker.KeyMap{
		GoToTop:  key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "first")),
		GoToLast: key.NewBinding(key.WithKeys("G"), key.WithHelp("G", "last")),
		Down:     key.NewBinding(key.WithKeys("j", "down", "ctrl+n"), key.WithHelp("j | ↓ ", "down")),
		Up:       key.NewBinding(key.WithKeys("k", "up", "ctrl+p"), key.WithHelp("k | ↑", "up")),
		PageUp:   key.NewBinding(key.WithKeys("K", "pgup"), key.WithHelp("pgup", "page up")),
		PageDown: key.NewBinding(key.WithKeys("J", "pgdown"), key.WithHelp("pgdown", "page down")),
		Back:     key.NewBinding(key.WithKeys("h", "backspace", "left"), key.WithHelp("h | ←", "back")),
		Open:     key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("l | →", "open")),
		// A bit of a hack. Filepicker impl looks for Select INSIDE a case statement checking for Open
		// They must share a key, but we want Enter to progress the system instead
		// The default keybindings has them sharing the Enter key
		Select: key.NewBinding(key.WithKeys("l", "right"), key.WithHelp("enter", "select")),
	}
}

// New initializes a new model and all of it's component pieces
func New() model {
	usernameTi := textinput.New()
	usernameTi.Focus()
	usernameTi.CharLimit = 128
	usernameTi.Width = 120

	identityTi := textinput.New()
	identityTi.CharLimit = 512
	identityTi.Width = 120

	fp := filepicker.New()
	fp.DirAllowed = true
	fp.FileAllowed = false
	fp.CurrentDirectory, _ = os.UserHomeDir()
	fp.Height = 20
	fp.KeyMap = directoryPickerKeyMap()

	items := []list.Item{}
	li := list.New(items, itemDelegate{}, 20, 14)
	li.Title = "Choose a file format"
	li.SetShowStatusBar(false)
	li.SetFilteringEnabled(false)

	filterTi := textinput.New()
	filterTi.CharLimit = 512
	filterTi.Width = 120

	return model{
		state:     showUsernameState,
		username:  usernameTi,
		identity:  identityTi,
		directory: fp,
		fileType:  li,
		filter:    filterTi,
		help:      help.New(),
		err:       nil,
		keys:      DefaultKeyMap(),
	}
}

// Init starts the TUI on the username
func (m model) Init() tea.Cmd {
	return m.username.Focus()
}

// ChangeState changes the model state and sets the next part of the UI
func (m *model) ChangeState(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.state {
	case showUsernameState:
		m.state = showIdentityState
		cmd = m.identity.Focus()
	case showIdentityState:
		m.state = showDirectoryPickerState
		cmd = m.directory.Init()
	case showDirectoryPickerState:
		m.state = showFormatListState
		items := []list.Item{
			item(internal.MP3_VO),
			item(internal.MP3_320),
			item(internal.WAV),
			item(internal.ALAC),
			item(internal.AIFF_LOSSLESS),
			item(internal.AAC_HI),
			item(internal.FLAC),
			item(internal.VORBIS),
		}

		cmd = m.fileType.SetItems(items)
	case showFormatListState:
		m.state = showFilterState
		cmd = m.filter.Focus()
	case showFilterState:
		cmd = tea.Quit

	}

	return m, cmd
}
