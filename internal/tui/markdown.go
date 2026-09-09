package tui

import (
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// Comments are written in markdown because the documents are: people reach for
// backticks around a field name and a dash for a list without thinking. Showing
// the source would make the review harder to read than the text it is about.
type renderer struct {
	inner *glamour.TermRenderer
	width int
}

// render returns the comment as it should appear, and false when markdown
// rendering is unavailable — a terminal too narrow to wrap into, or a renderer
// that would not build.
func (r *renderer) render(text string, width int) (string, bool) {
	if width < 20 {
		return "", false
	}
	if r.inner == nil || r.width != width {
		// Not WithAutoStyle: it asks the terminal for its background colour and
		// waits for a reply, which hangs the first frame on a terminal that
		// does not answer. lipgloss has already worked this out.
		style := "light"
		if lipgloss.HasDarkBackground() {
			style = "dark"
		}
		inner, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle(style),
			glamour.WithWordWrap(width),
		)
		if err != nil {
			return "", false
		}
		r.inner, r.width = inner, width
	}

	out, err := r.inner.Render(text)
	if err != nil {
		return "", false
	}
	// Glamour pads every line to the wrap width and frames the block in blank
	// lines; neither helps inside a pane that already has margins.
	lines := strings.Split(strings.Trim(out, "\n"), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " ")
	}
	return strings.Join(lines, "\n"), true
}
