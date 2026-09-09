package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Binding is one shortcut being configured.
type Binding struct {
	Label string
	Key   string
}

type keymapModel struct {
	bindings  []Binding
	cursor    int
	capturing bool
	typing    bool
	typed     string
	saved     bool
	conflicts func(string) string
	width     int
}

// ChooseKeys lets the reader press the combinations they want instead of
// guessing at Zed's notation. conflict is asked what a key is already bound to,
// so a clash is visible before it is written. Returns false if nothing is to be
// saved.
func ChooseKeys(bindings []Binding, conflict func(string) string) ([]Binding, bool, error) {
	m := keymapModel{bindings: bindings, conflicts: conflict}
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return nil, false, err
	}
	result, ok := final.(keymapModel)
	if !ok || !result.saved {
		return nil, false, nil
	}
	return result.bindings, true, nil
}

func (m keymapModel) Init() tea.Cmd { return nil }

func (m keymapModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case tea.KeyMsg:
		switch {
		case m.capturing:
			return m.capture(msg)
		case m.typing:
			return m.typeKey(msg)
		default:
			return m.browse(msg)
		}
	}
	return m, nil
}

func (m keymapModel) browse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		if m.cursor < len(m.bindings)-1 {
			m.cursor++
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
		}
	case "enter":
		m.capturing = true
	case "t":
		m.typing, m.typed = true, ""
	case "x":
		m.bindings[m.cursor].Key = ""
	case "s":
		m.saved = true
		return m, tea.Quit
	}
	return m, nil
}

// capture turns a real keypress into Zed's notation. A terminal cannot see
// every combination — cmd, and some ctrl pairs, never arrive — which is why
// typing one out stays available.
func (m keymapModel) capture(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "esc" {
		m.capturing = false
		return m, nil
	}
	m.bindings[m.cursor].Key = zedNotation(msg.String())
	m.capturing = false
	return m, nil
}

func (m keymapModel) typeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.typing = false
	case "enter":
		if typed := strings.TrimSpace(m.typed); typed != "" {
			m.bindings[m.cursor].Key = typed
		}
		m.typing = false
	case "backspace":
		if m.typed != "" {
			m.typed = m.typed[:len(m.typed)-1]
		}
	default:
		if len(msg.String()) == 1 {
			m.typed += msg.String()
		}
	}
	return m, nil
}

func zedNotation(key string) string {
	if key == " " || key == "space" {
		return "space"
	}
	return strings.ReplaceAll(key, "+", "-")
}

func (m keymapModel) View() string {
	var b strings.Builder
	b.WriteString("\n  " + titleStyle.Render("mdrev shortcuts") + "\n\n")

	for i, binding := range m.bindings {
		marker, render := "  ", lipgloss.NewStyle().Render
		if i == m.cursor {
			marker, render = "> ", selectedStyle.Render
		}
		key := binding.Key
		if key == "" {
			key = dimStyle.Render("none")
		}
		fmt.Fprintf(&b, "%s%-22s %s", marker, render(binding.Label), key)

		if binding.Key != "" && m.conflicts != nil {
			if bound := m.conflicts(binding.Key); bound != "" {
				b.WriteString("  " + conflictStyle.Render("already: "+bound))
			}
		}
		b.WriteString("\n")
	}

	switch {
	case m.capturing:
		b.WriteString("\n  " + quoteStyle.Render("press the combination…") +
			dimStyle.Render("  esc cancel") + "\n")
	case m.typing:
		b.WriteString("\n  type it: " + m.typed + "▏" +
			dimStyle.Render("   e.g. ctrl-alt-r · enter accept · esc cancel") + "\n")
	default:
		b.WriteString("\n  " + dimStyle.Render(
			"j/k move · enter press a key · t type one · x clear · s save · q cancel") + "\n")
	}
	return b.String()
}
