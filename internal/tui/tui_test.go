package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/esivres/mdrev/internal/mrsf"
)

// Opening the browser from the editor must land on the discussion about the
// spot the reader is looking at; landing on the first thread would make the
// shortcut useless on a long document.
func TestOpensTheThreadNearestTheCursor(t *testing.T) {
	m := model{threads: []thread{
		{parent: mrsf.Comment{Line: 10}, line: 10},
		{parent: mrsf.Comment{Line: 44}, line: 44},
		{parent: mrsf.Comment{Line: 61}, line: 61},
	}}

	for _, tc := range []struct {
		line int
		want int
	}{
		{line: 0, want: 0},  // no position known: first thread
		{line: 9, want: 0},  // just above a thread
		{line: 40, want: 1}, // between threads, nearer the second
		{line: 90, want: 2}, // past the last thread
	} {
		m.line = tc.line
		if got := m.nearestThread(); got != tc.want {
			t.Errorf("line %d: got thread %d, want %d", tc.line, got, tc.want)
		}
	}
}

const doc = `# Heading

The deduplication key is the tax number. When it is absent, a composite key
of name and address is used. Records without both are rejected.

## Non-functional

Latency p99 must not exceed 200 ms per request.
`

// A remark is unreadable without the sentence it is about, so the browser
// shows the whole paragraph, not just the quoted fragment.
func TestParagraphAroundTheQuote(t *testing.T) {
	got := paragraphAt(doc, 3, "composite key")

	want := "The deduplication key is the tax number. When it is absent, a composite key\n" +
		"of name and address is used. Records without both are rejected."
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

// The recorded line drifts as soon as text is inserted above it, so the quote
// wins over the line whenever it is still in the document.
func TestParagraphFollowsTheQuoteNotTheLine(t *testing.T) {
	if got := paragraphAt(doc, 1, "Latency p99"); got != "Latency p99 must not exceed 200 ms per request." {
		t.Errorf("got %q", got)
	}
}

// With the fragment gone the comment is orphaned, and the line is all that is
// left to show — better a stale paragraph than nothing.
func TestParagraphFallsBackToTheLine(t *testing.T) {
	if got := paragraphAt(doc, 8, "gone from the document"); got != "Latency p99 must not exceed 200 ms per request." {
		t.Errorf("got %q", got)
	}
}

// A review is written in sentences, and a terminal cuts whatever runs past its
// width with no sign that anything is missing — so the end of a remark would
// simply be invisible.
func TestLongProseIsWrappedToThePane(t *testing.T) {
	m := model{body: viewport.New(40, 10)}
	text := "Порог 2 не обоснован: для коротких наименований это почти всегда " +
		"ложное срабатывание, и его стоит пересмотреть."

	wrapped := m.wrap(text, "  ")

	lines := strings.Split(wrapped, "\n")
	if len(lines) < 3 {
		t.Errorf("want the text folded over several lines, got %d:\n%s", len(lines), wrapped)
	}
	for _, line := range lines {
		if width := lipgloss.Width(line); width > 40 {
			t.Errorf("line is %d wide, pane is 40: %q", width, line)
		}
		if !strings.HasPrefix(line, "  ") {
			t.Errorf("every line keeps the indent, got %q", line)
		}
	}
	if !strings.Contains(strings.Join(lines, " "), "пересмотреть") {
		t.Error("the end of the text must survive wrapping")
	}
}

// Paragraphs the author separated must stay separated.
func TestWrapKeepsBlankLinesBetweenParagraphs(t *testing.T) {
	m := model{body: viewport.New(40, 10)}

	wrapped := m.wrap("first paragraph\n\nsecond paragraph", "  ")

	if !strings.Contains(wrapped, "\n  \n") && !strings.Contains(wrapped, "\n\n") {
		t.Errorf("the blank line between paragraphs was lost:\n%q", wrapped)
	}
}

// Comments are written in markdown because the documents are: a field name in
// backticks and a dash for a list are how people write, and showing the source
// would make the review harder to read than the text it is about.
func TestCommentMarkdownIsRendered(t *testing.T) {
	m := model{body: viewport.New(60, 10)}

	out := m.prose("Порог `2` не обоснован:\n\n- «ООО Альфа» и «ООО Альба»\n- нужен другой ключ")
	// Styling is inserted mid-phrase, so the assertions read the plain text.
	plain := ansi.Strip(out)

	if strings.Contains(plain, "- «ООО Альфа»") {
		t.Errorf("the list marker was left as source:\n%s", plain)
	}
	if !strings.Contains(plain, "«ООО Альфа»") || !strings.Contains(plain, "нужен другой ключ") {
		t.Errorf("the text itself must survive:\n%s", plain)
	}
	for _, line := range strings.Split(out, "\n") {
		if width := lipgloss.Width(line); width > 60 {
			t.Errorf("line is %d wide, pane is 60: %q", width, line)
		}
	}
}

// A pane too narrow to render into must still show the comment.
func TestNarrowPaneFallsBackToPlainText(t *testing.T) {
	m := model{body: viewport.New(10, 10)}

	if out := m.prose("some remark"); !strings.Contains(out, "some remark") {
		t.Errorf("the comment must appear even when markdown cannot be rendered: %q", out)
	}
}

// Moving between threads and reading one want the same keys, so entering a
// thread hands the arrows to it and esc gives them back.
func TestReadingModeScrollsTheThreadAndEscReturns(t *testing.T) {
	m := model{
		threads: []thread{{parent: mrsf.Comment{ID: "a"}}, {parent: mrsf.Comment{ID: "b"}}},
		body:    viewport.New(40, 3),
	}
	m.body.SetContent(strings.Repeat("line\n", 40))

	entered, _ := m.updateBrowsing(press("enter"))
	reading := entered.(model)
	if reading.mode != readingMode() {
		t.Fatalf("enter must open the thread, mode is %v", reading.mode)
	}

	scrolled, _ := reading.updateReading(press("down"))
	if scrolled.(model).body.YOffset == 0 {
		t.Error("the arrows must scroll the thread while reading it")
	}
	if scrolled.(model).cursor != 0 {
		t.Error("scrolling must not move between threads")
	}

	back, _ := scrolled.(model).updateReading(press("esc"))
	if back.(model).mode != browsing {
		t.Error("esc must return to the list")
	}
}

func readingMode() mode { return reading }

// A markdown list runs for dozens of lines without a blank one, so taking the
// run between blank lines showed every thread in it the same wall of text.
// The unit is the item, and each thread gets its own.
func TestContextIsTheListItemNotTheWholeList(t *testing.T) {
	var doc strings.Builder
	for i := 1; i <= 40; i++ {
		fmt.Fprintf(&doc, "%d. item number %d in a long list\n", i, i)
	}

	fifth := paragraphAt(doc.String(), 5, "item number 5")
	thirtieth := paragraphAt(doc.String(), 30, "item number 30")

	if fifth == thirtieth {
		t.Fatal("two threads in the same list must not show identical context")
	}
	if fifth != "5. item number 5 in a long list" {
		t.Errorf("context should be the item alone, got %q", fifth)
	}
	if !strings.Contains(thirtieth, "item number 30") {
		t.Errorf("the anchored item must be its own context, got %q", thirtieth)
	}
}

// A table row is a record in itself; the rows around it are other records.
func TestContextIsTheTableRow(t *testing.T) {
	doc := "| Field | Rule |\n|---|---|\n| ИНН | 10 or 12 digits |\n| КПП | 9 digits |\n"

	got := paragraphAt(doc, 3, "10 or 12 digits")

	if got != "| ИНН | 10 or 12 digits |" {
		t.Errorf("context should be the row alone, got %q", got)
	}
}

// Half a code block says nothing, so a fenced block is quoted whole.
func TestContextIsTheWholeFencedBlock(t *testing.T) {
	doc := "Before.\n\n```go\nfunc main() {\n\tprintln(\"hi\")\n}\n```\n\nAfter.\n"

	got := paragraphAt(doc, 5, "println")

	if !strings.HasPrefix(got, "```go") || !strings.HasSuffix(got, "```") {
		t.Errorf("context should be the fenced block, got %q", got)
	}
}

// A unit that is itself enormous still has to fit on screen.
func TestEnormousUnitIsTrimmedWithAnEllipsis(t *testing.T) {
	var doc strings.Builder
	doc.WriteString("```\n")
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&doc, "line %d of a very long block\n", i)
	}
	doc.WriteString("```\n")

	got := paragraphAt(doc.String(), 31, "line 30 of a very long block")

	if lines := strings.Count(got, "\n") + 1; lines > maxContextLines+2 {
		t.Errorf("context is %d lines, which is a wall of text", lines)
	}
	if !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, "…") {
		t.Errorf("a trimmed context must say so, got %q", got)
	}
}
