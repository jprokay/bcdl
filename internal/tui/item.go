package tui

import (
	"fmt"
	"io"
	"strings"

	"bcdl/internal"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"

	tea "github.com/charmbracelet/bubbletea"
)

var (
	itemStyle         = lipgloss.NewStyle().PaddingLeft(4)
	selectedItemStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(lipgloss.Color("170"))
)

// Set up a custom list item
type item internal.FileType

// FilterValue returns the empty string. For our simple list, no filtering is allowed
func (i item) FilterValue() string { return "" }

type itemDelegate struct{}

// See the example for a [Simple List]
// [Simple List]: https://github.com/charmbracelet/bubbletea/blob/0af4525f516ab9150a1cfe5abb68d1fdc145a29c/examples/list-simple/main.go#L31
func (d itemDelegate) Height() int { return 1 }

// See the example for a [Simple List]
// [Simple List]: https://github.com/charmbracelet/bubbletea/blob/0af4525f516ab9150a1cfe5abb68d1fdc145a29c/examples/list-simple/main.go#L32
func (d itemDelegate) Spacing() int { return 0 }

// See the example for a [Simple List]
// [Simple List]: https://github.com/charmbracelet/bubbletea/blob/0af4525f516ab9150a1cfe5abb68d1fdc145a29c/examples/list-simple/main.go#L33
func (d itemDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }

// See the example for a [Simple List]
// [Simple List]: https://github.com/charmbracelet/bubbletea/blob/0af4525f516ab9150a1cfe5abb68d1fdc145a29c/examples/list-simple/main.go#L34
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
