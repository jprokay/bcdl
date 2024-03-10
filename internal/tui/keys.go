package tui

import "github.com/charmbracelet/bubbles/key"

// KeyMap sets up the Key Bindings for the application
type KeyMap struct {
	Quit    key.Binding
	Exit    key.Binding
	Confirm key.Binding
}

// DefaultKeyMap maps bindings to specific keys
func DefaultKeyMap() KeyMap {
	return KeyMap{
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
		Exit: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "quit"),
		),
		Confirm: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "confirm"),
		),
	}
}

// ShortHelp returns a shortened list of bindings to render for Help
func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Confirm, k.Quit, k.Exit}
}

// FullHelps returns the full list of bindings. This is required by
// the [Help Component]
// [Help Component]: https://github.com/charmbracelet/bubbletea/blob/master/examples/help/main.go
func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Confirm, k.Quit, k.Exit},
	}
}
