package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// Draft is what the reader wrote.
type Draft struct {
	Text     string
	Type     string
	Suggest  string
	Severity string
}

var commentTypes = []string{"", "issue", "question", "suggestion"}

const defaultWidth = 72

type composeModel struct {
	quote   string
	line    int
	typeAt  int
	body    textarea.Model
	suggest textarea.Model
	focus   int // 0 body, 1 suggestion
	saved   bool
	width   int
}

// Compose collects a comment: the text, what kind it is, and a replacement to
// propose. Reading it from stdin meant no editing and no way to offer a
// suggestion without knowing the flag.
func Compose(quote string, line int) (Draft, bool, error) {
	body := textarea.New()
	body.Placeholder = "What is wrong, or what you want to know."
	body.ShowLineNumbers = false
	body.SetHeight(6)
	// A width is set up front: a terminal that reports none would otherwise
	// render one character per line until the first resize.
	body.SetWidth(defaultWidth)
	body.Focus()

	suggest := textarea.New()
	suggest.Placeholder = "Replacement text (only for a suggestion)."
	suggest.ShowLineNumbers = false
	suggest.SetHeight(3)
	suggest.SetWidth(defaultWidth)

	m := composeModel{quote: quote, line: line, body: body, suggest: suggest}
	final, err := tea.NewProgram(m).Run()
	if err != nil {
		return Draft{}, false, err
	}
	result, ok := final.(composeModel)
	if !ok || !result.saved {
		return Draft{}, false, nil
	}
	return Draft{
		Text:    strings.TrimSpace(result.body.Value()),
		Type:    commentTypes[result.typeAt],
		Suggest: strings.TrimSpace(result.suggest.Value()),
	}, true, nil
}

func (m composeModel) Init() tea.Cmd { return textarea.Blink }

func (m composeModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		width := max(40, min(80, msg.Width-4))
		m.body.SetWidth(width)
		m.suggest.SetWidth(width)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "ctrl+c":
			return m, tea.Quit
		case "ctrl+s", "ctrl+d":
			if strings.TrimSpace(m.body.Value()) != "" {
				m.saved = true
			}
			return m, tea.Quit
		case "ctrl+t":
			m.typeAt = (m.typeAt + 1) % len(commentTypes)
			return m, nil
		case "tab":
			// The replacement field is only meaningful for a suggestion, and
			// switching to it says so without a separate step.
			m.focus = 1 - m.focus
			if m.focus == 1 {
				m.typeAt = indexOf(commentTypes, "suggestion")
				m.body.Blur()
				m.suggest.Focus()
			} else {
				m.suggest.Blur()
				m.body.Focus()
			}
			return m, textarea.Blink
		}
	}

	var cmd tea.Cmd
	if m.focus == 0 {
		m.body, cmd = m.body.Update(msg)
	} else {
		m.suggest, cmd = m.suggest.Update(msg)
	}
	return m, cmd
}

func (m composeModel) View() string {
	var b strings.Builder
	b.WriteString("\n  " + titleStyle.Render("New comment") + "\n")
	if m.quote != "" {
		b.WriteString("  on " + quoteStyle.Render(truncate(m.quote, 60)) + "\n")
	} else if m.line > 0 {
		b.WriteString("  " + dimStyle.Render(fmt.Sprintf("on line %d", m.line)) + "\n")
	}

	kind := m.draftType()
	b.WriteString("  " + dimStyle.Render("type: ") + kind + "\n\n")
	b.WriteString(m.body.View() + "\n")

	if commentTypes[m.typeAt] == "suggestion" {
		b.WriteString("\n  " + dimStyle.Render("replacement:") + "\n" + m.suggest.View() + "\n")
	}

	b.WriteString("\n  " + dimStyle.Render(
		"ctrl+s save · ctrl+t change type · tab propose a replacement · esc cancel") + "\n")
	return b.String()
}

func (m composeModel) draftType() string {
	if commentTypes[m.typeAt] == "" {
		return dimStyle.Render("plain remark")
	}
	return selectedStyle.Render(commentTypes[m.typeAt])
}

func indexOf(values []string, want string) int {
	for i, v := range values {
		if v == want {
			return i
		}
	}
	return 0
}
