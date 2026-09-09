package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Choice is one thing the reader can do at the cursor.
type Choice struct {
	Label  string
	Detail string
}

type pickModel struct {
	title   string
	choices []Choice
	cursor  int
	chosen  bool
}

// Pick asks which of several things to do. It exists because standing on a
// line that already carries a discussion, the useful action is usually to
// answer it — and having to reselect the text first to say so is silly.
func Pick(title string, choices []Choice) (int, bool, error) {
	final, err := tea.NewProgram(pickModel{title: title, choices: choices}).Run()
	if err != nil {
		return 0, false, err
	}
	m, ok := final.(pickModel)
	if !ok || !m.chosen {
		return 0, false, nil
	}
	return m.cursor, true, nil
}

func (m pickModel) Init() tea.Cmd { return nil }

func (m pickModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			if m.cursor < len(m.choices)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		case "enter", " ":
			m.chosen = true
			return m, tea.Quit
		default:
			// A digit picks directly, which is faster than moving for a list
			// this short.
			if n := int(key.String()[0] - '1'); len(key.String()) == 1 && n >= 0 && n < len(m.choices) {
				m.cursor, m.chosen = n, true
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

func (m pickModel) View() string {
	var b strings.Builder
	b.WriteString("\n  " + titleStyle.Render(m.title) + "\n\n")
	for i, choice := range m.choices {
		marker, render := "  ", lipgloss.NewStyle().Render
		if i == m.cursor {
			marker, render = "> ", selectedStyle.Render
		}
		fmt.Fprintf(&b, "%s%d) %s\n", marker, i+1, render(choice.Label))
		if choice.Detail != "" {
			b.WriteString("       " + dimStyle.Render(truncate(choice.Detail, 60)) + "\n")
		}
	}
	b.WriteString("\n  " + dimStyle.Render("j/k move · enter choose · esc cancel") + "\n")
	return b.String()
}
