package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/key"
)

// fpKeyMap is necessary to add additional helper methods
type fpKeyMap filepicker.KeyMap

// ShortHelp renders a set of shorter help options
func (k fpKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Select, k.Up, k.Down, k.Back, k.Open}
}

// Fullhelp renders all of the keys the file picker can use
func (k fpKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Back, k.Open, k.Select},
		{k.PageUp, k.PageDown, k.GoToTop, k.GoToLast},
	}
}

func (m model) textInputView(question, view string) string {
	return m.withHelp(fmt.Sprintf("%s\n\n%s", question, view))
}

func (m model) withHelp(s string) string {
	return fmt.Sprintf("%s\n\n%s", s, m.help.View(m.keys))
}

// View renders the appropriate view based on the current state
func (m model) View() string {
	var output string

	switch m.state {
	case showUsernameState:
		output = m.textInputView("What's your username?", m.username.View())
	case showIdentityState:
		output = m.textInputView("What's the value of your Identity cookie?", m.identity.View())
	case showDirectoryPickerState:
		var s strings.Builder

		if selected.Directory == "" {
			s.WriteString("Press Enter to select a directory to save your downloads:")
		} else {
			s.WriteString(fmt.Sprintf("You selected: %s", m.directory.Styles.Selected.Render(selected.Directory)))
		}

		s.WriteString(fmt.Sprintf("\n\n%s\n%s", m.directory.View(), m.help.View(fpKeyMap(m.directory.KeyMap))))
		output = s.String()
	case showFormatListState:
		output = m.withHelp(m.fileType.View())
	case showFilterState:
		output = m.textInputView("Filter collection (leave empty to download everything)?", m.filter.View())

	}
	return output
}
