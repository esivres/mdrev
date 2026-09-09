// Package tui shows a document's review threads in full — an editor's inline
// diagnostic is one line, which a discussion does not fit into.
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

	"github.com/esivres/mdrev/internal/anchor"
	"github.com/esivres/mdrev/internal/mrsf"
)

type thread struct {
	parent  mrsf.Comment
	replies []mrsf.Comment
	line    int // where the anchor actually is now, not where it was recorded
}

type mode int

const (
	browsing mode = iota
	reading       // inside a thread: the keys scroll it
	replying
	composing
)

type model struct {
	document string
	docText  string // read alongside the sidecar, to show the text under discussion
	line     int    // where the editor's cursor was, 0 when unknown
	threads  []thread
	cursor   int
	showAll  bool

	mode       mode
	body       viewport.Model
	editor     textarea.Model
	markdown   renderer
	status     string
	confirming bool   // esc left the editor; a single key decides what happens
	newID      string // thread to select after reloading, so a new comment opens
	ready      bool
	width      int
	height     int
	quitErr    error
}

var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	quoteStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	authorStyle   = lipgloss.NewStyle().Bold(true)
	suggestStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	conflictStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	contextStyle  = lipgloss.NewStyle().Faint(true).Border(lipgloss.NormalBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color("8")).PaddingLeft(1)
	listStyle = lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, true, false, false).
			BorderForeground(lipgloss.Color("8")).PaddingRight(2).MarginRight(2)
)

// Run opens the thread browser. A non-zero line is where the reader was, so
// the thread about that spot opens first.
func Run(document string, line int) error {
	m := model{document: document, line: line, editor: newEditor()}
	if err := m.reload(); err != nil {
		return err
	}
	m.cursor = m.nearestThread()
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
	ta.Placeholder = "Your reply. Esc when done."
	ta.ShowLineNumbers = false
	ta.SetHeight(6)
	return ta
}

func (m *model) reload() error {
	// The cursor is an index but the reader is looking at a thread.
	var selected string
	if m.cursor < len(m.threads) {
		selected = m.threads[m.cursor].parent.ID
	}

	if data, err := os.ReadFile(m.document); err == nil {
		m.docText = string(data)
	}
	sc, err := mrsf.Load(m.document)
	if err != nil {
		return err
	}
	m.threads = nil
	if sc == nil {
		m.cursor = 0
		return nil
	}

	for _, t := range sc.Threads(m.showAll) {
		m.threads = append(m.threads, thread{
			parent:  t.Parent,
			replies: t.Replies,
			line:    m.currentLine(t.Parent),
		})
	}

	for i, t := range m.threads {
		if t.parent.ID == selected {
			m.cursor = i
		}
	}
	if m.cursor >= len(m.threads) {
		m.cursor = max(0, len(m.threads)-1)
	}
	return nil
}

// The recorded line drifts as soon as text is inserted above it.
func (m model) currentLine(c mrsf.Comment) int {
	if m.docText == "" {
		return c.Line
	}
	lines := strings.Split(m.docText, "\n")
	return anchor.NearestLine(lines, c.SelectedText, c.Line-1) + 1
}

func (m model) nearestThread() int {
	if m.line == 0 {
		return 0
	}
	best, bestDist := 0, 1<<30
	for i, t := range m.threads {
		d := t.line - m.line
		if d < 0 {
			d = -d
		}
		if d < bestDist {
			best, bestDist = i, d
		}
	}
	return best
}

// Anchors a new comment on the line the reader came from.
func (m model) lineQuote() string {
	if m.line == 0 {
		return ""
	}
	data, err := os.ReadFile(m.document)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if m.line > len(lines) {
		return ""
	}
	return anchor.After(lines[m.line-1])
}

// contextLines is how much of the document is shown above a thread. A
// paragraph is the natural unit, but a markdown list or table runs for dozens
// of lines without a blank one, and then every thread inside it shows the same
// wall of text instead of the sentence it is about.
const contextLines = 3

// A remark cannot be judged without the sentence around it, and switching to
// the document to find it defeats the browser.
func paragraphAt(text string, line int, quote string) string {
	lines := strings.Split(text, "\n")
	idx := anchor.NearestLine(lines, quote, line-1)
	if idx < 0 || idx >= len(lines) {
		return ""
	}

	first, last := idx, idx
	for first > 0 && idx-first < contextLines && strings.TrimSpace(lines[first-1]) != "" {
		first--
	}
	for last < len(lines)-1 && last-idx < contextLines && strings.TrimSpace(lines[last+1]) != "" {
		last++
	}

	shown := strings.Join(lines[first:last+1], "\n")
	if first > 0 && strings.TrimSpace(lines[first-1]) != "" {
		shown = "…\n" + shown
	}
	if last < len(lines)-1 && strings.TrimSpace(lines[last+1]) != "" {
		shown += "\n…"
	}
	return shown
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
		switch m.mode {
		case reading:
			return m.updateReading(msg)
		case browsing:
			return m.updateBrowsing(msg)
		default:
			return m.updateComposing(msg)
		}
	}
	return m, nil
}

// A viewport built with a negative height renders nothing, silently.
func (m *model) layout() {
	listWidth := min(42, max(20, m.width/3))
	bodyWidth := max(20, m.width-listWidth-4)
	m.body = viewport.New(bodyWidth, max(3, m.height-5))
	m.body.KeyMap = viewport.DefaultKeyMap()
	m.editor.SetWidth(bodyWidth)
}

func (m model) updateBrowsing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc", "ctrl+c":
		return m, tea.Quit

	case "enter":
		// Reading a thread and moving between them want the same keys, so
		// entering one hands the arrows to it until esc.
		if len(m.threads) > 0 {
			m.mode = reading
		}

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

	case "n":
		m.mode = composing
		m.editor.Reset()
		m.editor.Placeholder = "New comment. Esc when done."
		m.editor.Focus()
		m.status = ""
		return m, textarea.Blink

	case "r":
		if len(m.threads) == 0 {
			return m, nil
		}
		m.mode = replying
		m.editor.Placeholder = "Your reply. Esc when done."
		m.editor.Reset()
		m.editor.Focus()
		m.status = ""
		return m, textarea.Blink

	case "x":
		cmd := m.toggleResolved()
		return m, cmd

	case "a":
		m.showAll = !m.showAll
		m.status = ""
		if err := m.reload(); err != nil {
			m.status = err.Error()
		}
		m.body.SetContent(m.threadView())

	case "o":
		cmd := m.openInEditor()
		return m, cmd

	}
	return m, nil
}

// updateReading scrolls the selected thread. The arrows and j/k belong to the
// thread here, which is why entering it is a mode rather than another chord.
func (m model) updateReading(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		m.mode = browsing
		return m, nil
	case "r", "n", "x", "o", "a":
		// The thread-level actions still work without going back first.
		m.mode = browsing
		return m.updateBrowsing(msg)
	}

	var cmd tea.Cmd
	m.body, cmd = m.body.Update(msg)
	return m, cmd
}

// Writing a new comment and replying share their keys.
func (m model) updateComposing(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.confirming {
		switch msg.String() {
		case "s", "enter":
			m.confirming = false
			return m.save()
		case "q":
			m.confirming = false
			m.mode = browsing
			return m, nil
		case "i", "a", "esc":
			m.confirming = false
			m.editor.Focus()
			return m, textarea.Blink
		}
		return m, nil
	}

	switch msg.String() {
	case "esc":
		// Same reason as the compose form: ctrl+s is flow control in a
		// terminal, so esc finishes and asks what to do with the text.
		if strings.TrimSpace(m.editor.Value()) == "" {
			m.mode = browsing
			m.editor.Blur()
			return m, nil
		}
		m.confirming = true
		m.editor.Blur()
		return m, nil
	case "ctrl+s", "ctrl+d":
		return m.save()
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	return m, cmd
}

func (m model) save() (tea.Model, tea.Cmd) {
	{
		text := strings.TrimSpace(m.editor.Value())
		if text == "" {
			m.mode = browsing
			m.editor.Blur()
			return m, nil
		}
		var err error
		if m.mode == composing {
			err = m.saveComment(text)
			m.status = "comment saved"
		} else {
			err = m.saveReply(text)
			m.status = "reply saved"
		}
		if err != nil {
			m.status = err.Error()
		}
		m.mode = browsing
		m.editor.Blur()
		if err := m.reload(); err != nil {
			m.status = err.Error()
		}
		if m.newID != "" {
			for i, t := range m.threads {
				if t.parent.ID == m.newID {
					m.cursor = i
				}
			}
			m.newID = ""
		}
		m.body.SetContent(m.threadView())
		return m, nil
	}
}

func (m *model) saveReply(text string) error {
	parent := m.threads[m.cursor].parent
	return mrsf.Update(m.document, func(sc *mrsf.Sidecar) error {
		// The sidecar may have been rewritten since it was loaded.
		if sc.Find(parent.ID) == nil {
			return fmt.Errorf("that thread is no longer in the review")
		}
		_, err := sc.Add(mrsf.Comment{
			Author:       mrsf.DefaultAuthor(),
			Text:         text,
			Line:         parent.Line,
			SelectedText: parent.SelectedText,
			ReplyTo:      parent.ID,
		})
		return err
	})
}

// saveComment opens a new thread on the line the reader came from.
func (m *model) saveComment(text string) error {
	quote := m.lineQuote()
	return mrsf.Update(m.document, func(sc *mrsf.Sidecar) error {
		added, err := sc.Add(mrsf.Comment{
			Author:       mrsf.DefaultAuthor(),
			Text:         text,
			Line:         m.line,
			SelectedText: quote,
		})
		if err == nil {
			m.newID = added.ID
		}
		return err
	})
}

func (m *model) toggleResolved() tea.Cmd {
	if len(m.threads) == 0 {
		return nil
	}
	id := m.threads[m.cursor].parent.ID

	var resolved bool
	if err := mrsf.Update(m.document, func(sc *mrsf.Sidecar) error {
		c := sc.Find(id)
		if c == nil {
			return fmt.Errorf("that thread is no longer in the review")
		}
		c.Resolved = !c.Resolved
		if c.Resolved {
			c.SetOutcome(mrsf.OutcomeResolved)
		} else {
			c.SetOutcome("")
		}
		resolved = c.Resolved
		return nil
	}); err != nil {
		m.status = err.Error()
		return nil
	}

	m.status = map[bool]string{true: "resolved", false: "reopened"}[resolved]
	if err := m.reload(); err != nil {
		m.status = err.Error()
	}
	m.body.SetContent(m.threadView())
	return nil
}

// Jumps from the discussion to the text it is about.
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
	if len(parts) == 0 {
		m.status = "set $EDITOR to open the document"
		return nil
	}
	target := fmt.Sprintf("%s:%d", m.document, m.threads[m.cursor].line)
	cmd := exec.Command(parts[0], append(parts[1:], target)...)
	return tea.ExecProcess(cmd, func(error) tea.Msg { return nil })
}

func (m model) View() string {
	if !m.ready {
		return "loading…"
	}
	if len(m.threads) == 0 && m.mode == browsing {
		return fmt.Sprintf("\n  %s\n\n  No open comments.\n\n  %s\n",
			titleStyle.Render(m.document),
			dimStyle.Render("n new · "+m.resolvedHelp()+" · q quit"))
	}

	counted := fmt.Sprintf("%d open", len(m.threads))
	if m.showAll {
		counted = fmt.Sprintf("%d threads, resolved included", len(m.threads))
	}
	header := fmt.Sprintf("  %s  %s",
		titleStyle.Render(m.document),
		dimStyle.Render(counted))

	panes := lipgloss.JoinHorizontal(lipgloss.Top,
		listStyle.Render(m.listView()),
		m.bodyPane())

	help := "j/k thread · enter read it · n new · r reply · x resolve · " +
		m.resolvedHelp() + " · o open in editor · q quit"
	switch {
	case m.confirming:
		help = "s save · i keep typing · q discard"
	case m.mode == reading:
		help = "↑/↓ scroll the thread · r reply · x resolve · o open in editor · esc back to the list"
	case m.mode != browsing:
		help = "esc when done"
	}
	footer := "  " + dimStyle.Render(help)
	if m.status != "" {
		footer += "  " + m.status
	}
	return header + m.scrollHint() + "\n" + panes + "\n" + footer
}

func (m model) resolvedHelp() string {
	if m.showAll {
		return "a hide resolved"
	}
	return "a show resolved"
}

// prose renders a comment as markdown, falling back to plain wrapping where
// that is not possible.
func (m *model) prose(text string) string {
	if out, ok := m.markdown.render(text, m.body.Width); ok {
		return out
	}
	return m.wrap(text, "  ")
}

// wrap folds prose to the pane. A review is written in sentences, and a
// terminal simply cuts anything past its width, so the end of a remark would
// be invisible with no sign that it is there.
func (m model) wrap(text, indent string) string {
	width := m.body.Width - lipgloss.Width(indent)
	if width < 20 {
		width = 20
	}
	style := lipgloss.NewStyle().Width(width)

	var out []string
	for _, paragraph := range strings.Split(text, "\n") {
		for _, line := range strings.Split(style.Render(paragraph), "\n") {
			out = append(out, indent+strings.TrimRight(line, " "))
		}
	}
	return strings.Join(out, "\n")
}

// A thread continuing past the pane is otherwise invisible.
func (m model) scrollHint() string {
	if m.mode != browsing || m.body.AtBottom() && m.body.AtTop() {
		return ""
	}
	return "  " + dimStyle.Render(fmt.Sprintf("scroll %3.0f%%", m.body.ScrollPercent()*100))
}

func (m model) listView() string {
	var b strings.Builder
	for i, t := range m.threads {
		quote := t.parent.SelectedText
		if quote == "" {
			quote = fmt.Sprintf("line %d", t.line)
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
	if m.mode == composing {
		header := "New comment"
		if q := m.lineQuote(); q != "" {
			header += " on " + quoteStyle.Render(q)
		}
		return header + "\n\n" + m.editor.View()
	}
	if m.mode == replying {
		return m.editor.View()
	}
	return m.body.View()
}

func (m *model) threadView() string {
	if len(m.threads) == 0 {
		return ""
	}
	t := m.threads[m.cursor]

	var b strings.Builder
	if para := paragraphAt(m.docText, t.line, t.parent.SelectedText); para != "" {
		if t.parent.SelectedText != "" {
			para = strings.Replace(para, t.parent.SelectedText,
				quoteStyle.Render(t.parent.SelectedText), 1)
		}
		b.WriteString(contextStyle.Width(m.body.Width).Render(para) + "\n")
	} else if t.parent.SelectedText != "" {
		b.WriteString(quoteStyle.Render(t.parent.SelectedText) + "\n")
	}
	b.WriteString(dimStyle.Render(fmt.Sprintf("%s:%d", m.document, t.line)) + "\n\n")

	writeComment := func(c mrsf.Comment) {
		b.WriteString(authorStyle.Render(c.Author) + "  " + dimStyle.Render(when(c.Timestamp)) + "\n")
		b.WriteString(m.prose(c.Text) + "\n\n")
	}
	writeComment(t.parent)
	if s, ok := t.parent.SuggestedText(); ok {
		b.WriteString(suggestStyle.Render(m.wrap("suggested: "+s, "")) + "\n\n")
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
