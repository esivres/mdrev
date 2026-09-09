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
		{parent: mrsf.Comment{Line: 10}},
		{parent: mrsf.Comment{Line: 44}},
		{parent: mrsf.Comment{Line: 61}},
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
