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

	diags := draftDiagnostics(li)
	if len(diags) != 1 {
		t.Fatalf("want 1 draft diagnostic, got %d", len(diags))
	}
	drafts := map[int]draft{}
	for _, d := range findDrafts(text) {
		drafts[d.Start] = d
	}

	action, ok := fileDraftAction("file:///doc.md", li, drafts, diags[0])
	if !ok {
		t.Fatal("no code action offered for a draft")
	}
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
