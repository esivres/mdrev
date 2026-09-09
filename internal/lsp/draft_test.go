package lsp

import "testing"

// A draft comment is filed against the words in front of it, because that is
// where a reader puts a remark — anchoring it anywhere else would attach the
// comment to text it is not about.
func TestDraftAnchorsOnPrecedingWords(t *testing.T) {
	text := "Latency p99 must not exceed 200 ms per request. {>>too optimistic<<}\n"

	drafts := findDrafts(text)
	if len(drafts) != 1 {
		t.Fatalf("want 1 draft, got %d", len(drafts))
	}
	if got, want := drafts[0].Text, "too optimistic"; got != want {
		t.Errorf("text: got %q, want %q", got, want)
	}
	if got, want := drafts[0].Anchor, "not exceed 200 ms per request."; got != want {
		t.Errorf("anchor: got %q, want %q", got, want)
	}
}

// With nothing before it, the draft has to look forward, otherwise a comment
// opening a paragraph would end up with no anchor at all.
func TestDraftOnItsOwnLineAnchorsForward(t *testing.T) {
	text := "## Deduplication\n\n{>>whose registry?<<} The deduplication key is the tax number.\n"

	drafts := findDrafts(text)
	if len(drafts) != 1 {
		t.Fatalf("want 1 draft, got %d", len(drafts))
	}
	if got, want := drafts[0].Anchor, "The deduplication key is the tax"; got != want {
		t.Errorf("anchor: got %q, want %q", got, want)
	}
}

// An empty marker is what a half-typed comment looks like; filing it would
// litter the sidecar with blank entries.
func TestEmptyDraftIsIgnored(t *testing.T) {
	if got := findDrafts("Some text {>>  <<} more\n"); len(got) != 0 {
		t.Errorf("want no drafts, got %+v", got)
	}
}

// Filing must remove the marker from the document, or the comment would exist
// twice: once in the sidecar and once as leftover markup.
func TestFilingRemovesTheMarkerAndItsSpace(t *testing.T) {
	text := "Latency p99 must not exceed 200 ms. {>>too optimistic<<}\n"
	li := newLineIndex(text)

	drafts := findDrafts(text)
	if len(drafts) != 1 {
		t.Fatalf("want 1 draft, got %d", len(drafts))
	}

	action := fileDraftAction("file:///doc.md", li, drafts[0])
	edits := action.Edit.Changes["file:///doc.md"]
	if len(edits) != 1 || edits[0].NewText != "" {
		t.Fatalf("want a single deletion, got %+v", edits)
	}
	if got, want := edits[0].Range.Start.Character, len("Latency p99 must not exceed 200 ms."); got != want {
		t.Errorf("deletion must swallow the space before the marker: got %d, want %d", got, want)
	}
	if got, want := edits[0].Range.End.Character, len(text)-1; got != want {
		t.Errorf("deletion must cover the whole marker: got %d, want %d", got, want)
	}
}

// The editor asks for actions at the cursor, an empty range, so an action must
// still be offered when the cursor merely sits inside the anchored text.
func TestActionsMatchAnEmptyCursorRange(t *testing.T) {
	inside := Range{Start: Position{Line: 0, Character: 40}, End: Position{Line: 0, Character: 40}}
	marker := Range{Start: Position{Line: 0, Character: 36}, End: Position{Line: 0, Character: 56}}
	if !overlaps(marker, inside) {
		t.Error("a cursor inside the range must match")
	}

	elsewhere := Range{Start: Position{Line: 4, Character: 0}, End: Position{Line: 4, Character: 0}}
	if overlaps(marker, elsewhere) {
		t.Error("a cursor on another line must not match")
	}
}

// A stray opener must not pair with a closer further down the file: the code
// action deletes whatever the marker covers, so a mistyped "{>>" would erase
// the paragraphs between it and the next real comment.
func TestStrayOpenerDoesNotSwallowTheDocument(t *testing.T) {
	drafts := findDrafts("{>>stray\n\nA paragraph of prose. {>>real remark<<}\n")

	if len(drafts) != 1 {
		t.Fatalf("want only the terminated marker, got %d: %+v", len(drafts), drafts)
	}
	if drafts[0].Text != "real remark" {
		t.Errorf("wrong draft matched: %q", drafts[0].Text)
	}
}

// A marker inside another marker would otherwise close on the inner tag and
// leave the outer tail behind as stray markup.
func TestNestedMarkersAreNotFiled(t *testing.T) {
	if got := findDrafts("{>>outer {>>inner<<} tail<<}"); len(got) != 1 || got[0].Text != "inner" {
		t.Errorf("want only the inner marker, got %+v", got)
	}
}

// Multi-line comments stay supported: the restriction is a blank line, not a
// newline.
func TestDraftMaySpanLines(t *testing.T) {
	got := findDrafts("Some prose. {>>first line\nsecond line<<}\n")
	if len(got) != 1 || got[0].Text != "first line\nsecond line" {
		t.Errorf("multi-line draft must be kept, got %+v", got)
	}
}
