package mrsf

import (
	"strconv"
	"testing"
)

const doc = "# Title\n\nThe threshold is not more than 2 today.\n\nA diagram mentions not more than 2 as well.\n"

// A recorded position that is never corrected describes where the text used to
// be. Nothing downstream can tell that from where it is now, so every later
// decision — which thread the cursor is in, which occurrence to rewrite — is
// made on a stale number.
func TestReanchorFollowsTheTextAndRecordsColumns(t *testing.T) {
	c := Comment{Line: 1, SelectedText: "not more than 2"}
	reanchor(doc, &c)

	if c.Line != 3 || c.EndLine != 3 {
		t.Errorf("line: got %d-%d, want 3-3", c.Line, c.EndLine)
	}
	if c.StartColumn == nil || *c.StartColumn != 17 {
		t.Errorf("start column: got %s, want 17", column(c.StartColumn))
	}
	if c.EndColumn == nil || *c.EndColumn != 32 {
		t.Errorf("end column: got %s, want 32", column(c.EndColumn))
	}
	if c.Orphaned() {
		t.Error("a fragment that is present must not be marked orphaned")
	}
}

// Columns are counted in characters, not bytes: the reference tool does, and a
// Cyrillic document would otherwise record positions nothing can use.
func TestColumnsCountCharactersNotBytes(t *testing.T) {
	c := Comment{Line: 1, SelectedText: "не превышает 2"}
	reanchor("Расстояние не превышает 2 сегодня.\n", &c)

	if c.StartColumn == nil || *c.StartColumn != 11 {
		t.Errorf("start column: got %s, want 11", column(c.StartColumn))
	}
}

// The same words often appear twice — in prose and again inside a diagram — so
// the recorded line decides which one the comment meant.
func TestReanchorPrefersTheOccurrenceNearestTheRecordedLine(t *testing.T) {
	c := Comment{Line: 5, SelectedText: "not more than 2"}
	reanchor(doc, &c)

	if c.Line != 5 {
		t.Errorf("want the occurrence near line 5, got line %d", c.Line)
	}
}

// A comment whose text is gone must say so, or an agent answers a thread about
// a passage that no longer exists without knowing it.
func TestVanishedFragmentIsMarkedOrphaned(t *testing.T) {
	c := Comment{Line: 3, SelectedText: "a sentence that was deleted"}
	reanchor(doc, &c)

	if !c.Orphaned() {
		t.Error("a fragment that is gone must be marked orphaned")
	}
	if c.Line != 3 {
		t.Errorf("an orphan keeps its last known line, got %d", c.Line)
	}
}

// The hash is documented as maintained by the commands, but was only ever
// computed on Add — a comment another tool wrote arrived without one and kept
// it that way.
func TestReanchorFillsAMissingHash(t *testing.T) {
	c := Comment{Line: 3, SelectedText: "not more than 2"}
	reanchor(doc, &c)

	if c.SelectedTextHash == "" {
		t.Error("hash must be filled in")
	}
}

func column(p *int) string {
	if p == nil {
		return "unset"
	}
	return strconv.Itoa(*p)
}
