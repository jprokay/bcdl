package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// Run sets up and executes the Bubble Tea UI and returns
// the values the user selected
func Run() (Outputs, error) {
	model := New()
	p := tea.NewProgram(model)

	if _, err := p.Run(); err != nil {
		return selected, err
	}

	return selected, nil
}
