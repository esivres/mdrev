package tui

import (
	"testing"

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
