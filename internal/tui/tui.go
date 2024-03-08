package tui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"download/internal"
	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"
)

type modelState uint

const (
	usernameView modelState = iota
	identityView
	filePickerView
	formatListView
	downloadProgressView
)

var (
	itemStyle         = lipgloss.NewStyle().PaddingLeft(4)
	selectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))
)

type item internal.FileType

func (i item) FilterValue() string { return "" }

type result struct {
	jobName string
}
type model struct {
	state      modelState
	username   textinput.Model
	identity   textinput.Model
	filepicker filepicker.Model
	formatList list.Model

	format internal.FileType

	help            help.Model
	downloadDir     string
	enteredUsername string
	enteredIdentity string

	err error
}

// Remap filepicker keys
func defaultKeyMap() filepicker.KeyMap {
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

type itemDelegate struct{}

func (d itemDelegate) Height() int                             { return 1 }
func (d itemDelegate) Spacing() int                            { return 0 }
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d itemDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(item)
	if !ok {
		return
	}

	str := fmt.Sprintf("%d. %s", index+1, i)

	fn := itemStyle.Render
	if index == m.Index() {
		fn = func(s ...string) string {
			return selectedItemStyle.Render("> " + strings.Join(s, " "))
		}
	}

	fmt.Fprint(w, fn(str))
}

var (
	enteredUsername, enteredIdentity, downloadDir string
	fileFormat                                    internal.FileType
)

func initialModel() model {
	usernameTi := textinput.New()
	usernameTi.Focus()
	usernameTi.CharLimit = 128
	usernameTi.Width = 80

	identityTi := textinput.New()
	identityTi.CharLimit = 512
	identityTi.Width = 120

	fp := filepicker.New()
	fp.DirAllowed = true
	fp.FileAllowed = false
	fp.CurrentDirectory, _ = os.UserHomeDir()
	fp.Height = 20
	fp.KeyMap = defaultKeyMap()

	items := []list.Item{}
	li := list.New(items, itemDelegate{}, 20, 14)
	li.Title = "Choose a file format"
	li.SetShowStatusBar(false)
	li.SetFilteringEnabled(false)

	return model{
		state:      usernameView,
		username:   usernameTi,
		identity:   identityTi,
		filepicker: fp,
		formatList: li,
		help:       help.New(),
		err:        nil,
	}
}

func (m model) Init() tea.Cmd {
	return textinput.Blink
}

type (
	errMsg error
)

type successMsg string
type inProgressMsg string
type failedMsg string

func (m model) changeState(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.state == usernameView {
		m.state = identityView
		return m, m.identity.Focus()
	} else if m.state == identityView {
		m.state = filePickerView
		return m, m.filepicker.Init()
	} else if m.state == filePickerView {
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

		m.state = formatListView
		return m, m.formatList.SetItems(items)
	} else {
		return m, tea.Quit
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			return m.changeState(msg)
		}
	case errMsg:
		m.err = msg
		return m, nil

	}

	if m.state == usernameView {
		m.username, cmd = m.username.Update(msg)
		m.enteredUsername = m.username.Value()
		enteredUsername = m.username.Value()
	} else if m.state == identityView {
		m.identity, cmd = m.identity.Update(msg)
		m.enteredIdentity = m.identity.Value()
		enteredIdentity = m.identity.Value()
	} else if m.state == filePickerView {
		m.filepicker, cmd = m.filepicker.Update(msg)

		if didSelect, path := m.filepicker.DidSelectFile(msg); didSelect {
			m.downloadDir = path
			downloadDir = path
		}

	} else if m.state == formatListView {
		m.formatList, cmd = m.formatList.Update(msg)
		i, ok := m.formatList.SelectedItem().(item)
		if ok {
			m.format = internal.FileType(i)
			fileFormat = internal.FileType(i)
		}
	}
	return m, cmd
}

type keyMap filepicker.KeyMap

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Select, k.Up, k.Down, k.Back, k.Open}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Back, k.Open, k.Select},
		{k.PageUp, k.PageDown, k.GoToTop, k.GoToLast},
	}
}

const exitText string = "(ESC or Ctrl+C to quit)"

func (m model) View() string {

	if m.state == usernameView {
		return fmt.Sprintf(
			"What's your username?\n\n%s\n\n%s", m.username.View(), exitText)
	} else if m.state == identityView {
		return fmt.Sprintf("Copy and paste the value for the Identity cookie:\n\n%s\n\n%s", m.identity.View(), exitText)
	} else if m.state == filePickerView {
		var s strings.Builder
		if m.downloadDir == "" {
			s.WriteString("Select a directory to save downloads:")
		} else {
			s.WriteString("Selected directory: " + m.filepicker.Styles.Selected.Render(m.downloadDir))
		}
		s.WriteString("\n\n" + m.filepicker.View() + "\n")
		s.WriteString(m.help.View(keyMap(m.filepicker.KeyMap)))
		return s.String()
	} else {
		return m.formatList.View()
	}
}

type SelectedOptions struct {
	Username    string
	Identity    string
	DownloadDir string
	Filetype    internal.FileType
}

func Run() (SelectedOptions, error) {
	model := initialModel()
	p := tea.NewProgram(model)

	if _, err := p.Run(); err != nil {
		return SelectedOptions{}, err
	}

	selected := SelectedOptions{
		Username:    enteredUsername,
		Identity:    enteredIdentity,
		DownloadDir: downloadDir,
		Filetype:    fileFormat,
	}

	return selected, nil
}
