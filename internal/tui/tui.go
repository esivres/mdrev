// Package tui presents review threads for one document: the whole discussion
// at once, which an editor's inline diagnostics cannot show.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/esivres/mdrev/internal/mrsf"
)

type thread struct {
	parent  mrsf.Comment
	replies []mrsf.Comment
}

type mode int

const (
	browsing mode = iota
	replying
)

type model struct {
	document string
	threads  []thread
	cursor   int
	showAll  bool

	mode    mode
	body    viewport.Model
	editor  textarea.Model
	status  string
	ready   bool
	width   int
	height  int
	quitErr error
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	quoteStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	authorStyle   = lipgloss.NewStyle().Bold(true)
	suggestStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	listStyle     = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(lipgloss.Color("8")).PaddingRight(2).MarginRight(2)
)

// Run opens the browser for a document's review threads.
func Run(document string) error {
	m := model{document: document, editor: newEditor()}
	if err := m.reload(); err != nil {
		return err
	}
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return err
	}
	if f, ok := final.(model); ok {
		return f.quitErr
	}
	return nil
}

func newEditor() textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Your reply. Ctrl+D to send, Esc to cancel."
	ta.ShowLineNumbers = false
	ta.SetHeight(6)
	return ta
}

func (m *model) reload() error {
	sc, err := mrsf.Load(m.document)
	if err != nil {
		return err
	}
	m.threads = nil
	if sc == nil {
		return nil
	}

	byID := map[string]int{}
	for _, c := range sc.Comments {
		if c.ReplyTo != "" {
			continue
		}
		if c.Resolved && !m.showAll {
			continue
		}
		byID[c.ID] = len(m.threads)
		m.threads = append(m.threads, thread{parent: c})
	}
	for _, c := range sc.Comments {
		if i, ok := byID[c.ReplyTo]; ok {
			m.threads[i].replies = append(m.threads[i].replies, c)
		}
	}
	if m.cursor >= len(m.threads) {
		m.cursor = max(0, len(m.threads)-1)
	}
	return nil
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		m.ready = true
		m.body.SetContent(m.threadView())
		return m, nil

	case tea.KeyMsg:
		if m.mode == replying {
			return m.updateReplying(msg)
		}
		return m.updateBrowsing(msg)
	}
	return m, nil
}

// layout guards against degenerate terminal sizes: a viewport built with a
// negative height renders nothing at all, silently.
func (m *model) layout() {
	listWidth := min(42, max(20, m.width/3))
	bodyWidth := max(20, m.width-listWidth-4)
	m.body = viewport.New(bodyWidth, max(3, m.height-4))
	m.editor.SetWidth(bodyWidth)
}

func (m model) updateBrowsing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit

	case "j", "down":
		if m.cursor < len(m.threads)-1 {
			m.cursor++
			m.body.SetContent(m.threadView())
			m.body.GotoTop()
		}
	case "k", "up":
		if m.cursor > 0 {
			m.cursor--
			m.body.SetContent(m.threadView())
			m.body.GotoTop()
		}

	case "r":
		if len(m.threads) == 0 {
			return m, nil
		}
		m.mode = replying
		m.editor.Reset()
		m.editor.Focus()
		m.status = ""
		return m, textarea.Blink

	case "x":
		return m, m.toggleResolved()

	case "a":
		m.showAll = !m.showAll
		m.status = ""
		if err := m.reload(); err != nil {
			m.status = err.Error()
		}
		m.body.SetContent(m.threadView())

	case "o":
		return m, m.openInEditor()

	default:
		var cmd tea.Cmd
		m.body, cmd = m.body.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) updateReplying(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = browsing
		m.editor.Blur()
		return m, nil
	case "ctrl+d":
		text := strings.TrimSpace(m.editor.Value())
		if text == "" {
			m.mode = browsing
			m.editor.Blur()
			return m, nil
		}
		if err := m.saveReply(text); err != nil {
			m.status = err.Error()
		} else {
			m.status = "reply saved"
		}
		m.mode = browsing
		m.editor.Blur()
		if err := m.reload(); err != nil {
			m.status = err.Error()
		}
		m.body.SetContent(m.threadView())
		return m, nil
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	return m, cmd
}

func (m *model) saveReply(text string) error {
	sc, err := mrsf.Load(m.document)
	if err != nil {
		return err
	}
	parent := m.threads[m.cursor].parent
	if _, err := sc.Add(mrsf.Comment{
		Author:       author(),
		Text:         text,
		Line:         parent.Line,
		SelectedText: parent.SelectedText,
		ReplyTo:      parent.ID,
	}); err != nil {
		return err
	}
	return sc.Save()
}

func (m *model) toggleResolved() tea.Cmd {
	if len(m.threads) == 0 {
		return nil
	}
	sc, err := mrsf.Load(m.document)
	if err != nil {
		m.status = err.Error()
		return nil
	}
	c := sc.Find(m.threads[m.cursor].parent.ID)
	if c == nil {
		return nil
	}
	c.Resolved = !c.Resolved
	if err := sc.Save(); err != nil {
		m.status = err.Error()
		return nil
	}
	m.status = map[bool]string{true: "resolved", false: "reopened"}[c.Resolved]
	if err := m.reload(); err != nil {
		m.status = err.Error()
	}
	m.body.SetContent(m.threadView())
	return nil
}

// openInEditor hands the document to $EDITOR at the thread's line, so a reader
// can jump from the discussion to the text it is about.
func (m *model) openInEditor() tea.Cmd {
	editor := os.Getenv("VISUAL")
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" || len(m.threads) == 0 {
		m.status = "set $EDITOR to open the document"
		return nil
	}
	parts := strings.Fields(editor)
	target := fmt.Sprintf("%s:%d", m.document, m.threads[m.cursor].parent.Line)
	cmd := exec.Command(parts[0], append(parts[1:], target)...)
	return tea.ExecProcess(cmd, func(error) tea.Msg { return nil })
}

func (m model) View() string {
	if !m.ready {
		return "loading…"
	}
	if len(m.threads) == 0 {
		return fmt.Sprintf("\n  %s\n\n  No open comments.\n\n  %s\n",
			titleStyle.Render(m.document), dimStyle.Render("a all · q quit"))
	}

	header := fmt.Sprintf("  %s  %s",
		titleStyle.Render(m.document),
		dimStyle.Render(fmt.Sprintf("%d threads", len(m.threads))))

	panes := lipgloss.JoinHorizontal(lipgloss.Top,
		listStyle.Render(m.listView()),
		m.bodyPane())

	help := "j/k move · r reply · x resolve · a all · o open · q quit"
	if m.mode == replying {
		help = "ctrl+d send · esc cancel"
	}
	footer := "  " + dimStyle.Render(help)
	if m.status != "" {
		footer += "  " + m.status
	}
	return header + "\n" + panes + "\n" + footer
}

func (m model) listView() string {
	var b strings.Builder
	for i, t := range m.threads {
		quote := t.parent.SelectedText
		if quote == "" {
			quote = fmt.Sprintf("line %d", t.parent.Line)
		}
		meta := t.parent.Author
		if t.parent.Type != "" {
			meta += " · " + t.parent.Type
		}
		switch n := len(t.replies); {
		case n == 1:
			meta += " · 1 reply"
		case n > 1:
			meta += fmt.Sprintf(" · %d replies", n)
		}
		if t.parent.Resolved {
			meta += " · resolved"
		}

		marker := "  "
		render := lipgloss.NewStyle().Render
		if i == m.cursor {
			marker = "> "
			render = selectedStyle.Render
		}
		b.WriteString(marker + render(truncate(quote, 36)) + "\n")
		b.WriteString("  " + dimStyle.Render(truncate(meta, 36)) + "\n\n")
	}
	return b.String()
}

func (m model) bodyPane() string {
	if m.mode == replying {
		return m.editor.View()
	}
	return m.body.View()
}

func (m model) threadView() string {
	if len(m.threads) == 0 {
		return ""
	}
	t := m.threads[m.cursor]

	var b strings.Builder
	if t.parent.SelectedText != "" {
		b.WriteString(quoteStyle.Render(t.parent.SelectedText) + "\n")
	}
	b.WriteString(dimStyle.Render(fmt.Sprintf("%s:%d", m.document, t.parent.Line)) + "\n\n")

	writeComment := func(c mrsf.Comment) {
		b.WriteString(authorStyle.Render(c.Author) + "  " + dimStyle.Render(when(c.Timestamp)) + "\n")
		for _, line := range strings.Split(c.Text, "\n") {
			b.WriteString("  " + line + "\n")
		}
		b.WriteString("\n")
	}
	writeComment(t.parent)
	if s, ok := t.parent.SuggestedText(); ok {
		b.WriteString(suggestStyle.Render("suggested: "+s) + "\n\n")
	}
	for _, r := range t.replies {
		writeComment(r)
	}
	return b.String()
}

func when(timestamp string) string {
	ts, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return ""
	}
	return ts.Local().Format("2006-01-02 15:04")
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func author() string {
	out, err := exec.Command("git", "config", "user.name").Output()
	if err == nil {
		if name := strings.TrimSpace(string(out)); name != "" {
			return name
		}
	}
	return os.Getenv("USER")
}
