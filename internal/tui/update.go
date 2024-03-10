package tui

import (
	"bcdl/internal"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// Update checks first for any matches against the [KeyMap].
// Otherwise, the message is passed along to the appropriate model.
func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit, m.keys.Exit):
			return m, tea.Quit
		case key.Matches(msg, m.keys.Confirm):
			return m.ChangeState(msg)
		}
	}

	switch m.state {
	case showUsernameState:
		m.username, cmd = m.username.Update(msg)
		selected.Username = m.username.Value()
	case showIdentityState:
		m.identity, cmd = m.identity.Update(msg)
		selected.Identity = m.identity.Value()
	case showDirectoryPickerState:
		m.directory, cmd = m.directory.Update(msg)

		if didSelect, path := m.directory.DidSelectFile(msg); didSelect {
			selected.Directory = path
		}
	case showFormatListState:
		m.fileType, cmd = m.fileType.Update(msg)
		i, ok := m.fileType.SelectedItem().(item)
		if ok {
			selected.FileType = internal.FileType(i)
		}
	case showFilterState:
		m.filter, cmd = m.filter.Update(msg)
		selected.Filter = m.filter.Value()

	}

	return m, cmd
}
