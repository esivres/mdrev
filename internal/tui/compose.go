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
	focus   int  // 0 body, 1 suggestion
	command bool // esc leaves the text and single keys act
	saved   bool
	width   int
}

// composeStart builds the form without running it, so its behaviour can be
// driven directly.
func composeStart(quote string, line int) (composeModel, bool, error) {
	body := textarea.New()
	body.ShowLineNumbers = false
	body.SetHeight(6)
	body.SetWidth(defaultWidth)
	body.Focus()

	suggest := textarea.New()
	suggest.ShowLineNumbers = false
	suggest.SetHeight(3)
	suggest.SetWidth(defaultWidth)

	return composeModel{quote: quote, line: line, body: body, suggest: suggest}, false, nil
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
		if m.command {
			return m.runCommand(msg)
		}
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			// Terminals eat ctrl+s as flow control and shells claim ctrl+t and
			// ctrl+d, so finishing is a mode rather than a chord.
			m.command = true
			m.body.Blur()
			m.suggest.Blur()
			return m, nil
		case "tab":
			return m.toggleSuggestion()
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

func (m composeModel) runCommand(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "s", "enter":
		if strings.TrimSpace(m.body.Value()) != "" {
			m.saved = true
		}
		return m, tea.Quit
	case "q", "esc", "ctrl+c":
		return m, tea.Quit
	case "t":
		m.typeAt = (m.typeAt + 1) % len(commentTypes)
		return m, nil
	case "r":
		m.command = false
		return m.toggleSuggestion()
	case "i", "a":
		m.command = false
		if m.focus == 1 {
			m.suggest.Focus()
		} else {
			m.body.Focus()
		}
		return m, textarea.Blink
	}
	return m, nil
}

// The replacement field is only meaningful for a suggestion, so opening it sets
// the type too.
func (m composeModel) toggleSuggestion() (tea.Model, tea.Cmd) {
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

	if m.command {
		b.WriteString("\n  " + selectedStyle.Render("s save") +
			dimStyle.Render(" · t change type · r propose a replacement · i keep typing · q discard") + "\n")
	} else {
		b.WriteString("\n  " + dimStyle.Render("esc when done · tab propose a replacement") + "\n")
	}
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
