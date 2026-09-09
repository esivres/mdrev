package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/esivres/mdrev/internal/mrsf"
)

const doc = `# Заголовок

Первый абзац, который потом сдвинется вниз.

Наименование считается совпавшим, если расстояние не превышает 2.
`

const sidecar = `mrsf_version: "1.0"
document: doc.md
comments:
  - id: c1
    author: Claude
    text: Порог не обоснован.
    resolved: false
    line: 2
    type: issue
    severity: high
    selected_text: не превышает 2
    x_suggested_text: не превышает 1
  - id: c2
    author: Claude
    text: Уже учтено.
    resolved: true
    line: 3
    selected_text: Первый абзац
`

// The recorded line (2) is deliberately wrong for the current document: a
// reviewer's comment must survive edits that shift the text, otherwise every
// comment turns stale the moment the author starts writing.
func TestDiagnosticsFollowTextNotRecordedLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	write(t, path, doc)
	write(t, path+".review.yaml", sidecar)

	diags := diagnosticsFor(t, path, doc)

	if len(diags) != 1 {
		t.Fatalf("want 1 open diagnostic (resolved ones must stay hidden), got %d: %+v", len(diags), diags)
	}
	d := diags[0]
	if d.Code != "c1" {
		t.Errorf("diagnostic must carry the comment id so code actions can find it, got %q", d.Code)
	}
	if d.Range.Start.Line != 4 {
		t.Errorf("anchor should follow the text to line 4, got %d", d.Range.Start.Line)
	}
	// "Наименование считается совпавшим, если расстояние " is 50 runes.
	if d.Range.Start.Character != 50 {
		t.Errorf("character offset must be counted in UTF-16 units over Cyrillic text, got %d", d.Range.Start.Character)
	}
	if !strings.Contains(d.Message, "Порог не обоснован.") {
		t.Errorf("message must carry the comment text, got %q", d.Message)
	}
	if !strings.Contains(d.Message, "не превышает 1") {
		t.Errorf("a suggestion must show its replacement text, got %q", d.Message)
	}
}

// A comment whose anchor text is gone must stay visible rather than disappear
// silently — a lost comment is worse than a misplaced one.
func TestOrphanedCommentStaysVisible(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	edited := strings.Replace(doc, "не превышает 2", "укладывается в порог", 1)
	write(t, path, edited)
	write(t, path+".review.yaml", sidecar)

	diags := diagnosticsFor(t, path, edited)

	if len(diags) != 1 {
		t.Fatalf("want the comment kept, got %d", len(diags))
	}
	if !strings.Contains(diags[0].Message, "anchor lost") {
		t.Errorf("orphaned anchor must be flagged, got %q", diags[0].Message)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// diagnosticsFor drives the server over its real stdio protocol so the test
// covers framing and dispatch, not just the diagnostic computation.
func diagnosticsFor(t *testing.T, path, text string) []Diagnostic {
	t.Helper()
	uri := "file://" + path

	var in bytes.Buffer
	frame(&in, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)
	open, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "method": "textDocument/didOpen",
		"params": map[string]any{"textDocument": map[string]any{"uri": uri, "text": text}},
	})
	frame(&in, string(open))

	var out bytes.Buffer
	if err := NewServer(&out).Run(&in); err != nil {
		t.Fatal(err)
	}

	br := bufio.NewReader(&out)
	for {
		msg, err := readMessage(br)
		if err != nil {
			t.Fatal("no publishDiagnostics notification was sent")
		}
		if msg.Method != "textDocument/publishDiagnostics" {
			continue
		}
		var p struct {
			Diagnostics []Diagnostic `json:"diagnostics"`
		}
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			t.Fatal(err)
		}
		return p.Diagnostics
	}
}

func frame(buf *bytes.Buffer, body string) {
	fmt.Fprintf(buf, "Content-Length: %d\r\n\r\n%s", len(body), body)
}

// A suggestion that has been applied must close its thread, however it was
// applied: by the code action, or by the author typing the change. Otherwise
// resolved work keeps showing up as open review.
func TestAppliedSuggestionClosesItsThread(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	write(t, path, doc)
	write(t, path+".review.yaml", sidecar)

	edited := strings.Replace(doc, "не превышает 2", "не превышает 1", 1)
	s := NewServer(&bytes.Buffer{})
	s.setDoc("file://"+path, edited)
	s.closeAppliedSuggestions("file://" + path)

	sc, err := mrsf.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c := sc.Find("c1"); c == nil || !c.Resolved {
		t.Error("thread must be resolved once its suggested text is in the document")
	}
}

// While the original text is still there the suggestion is merely proposed,
// and closing it would hide a decision the author has not made.
func TestUnappliedSuggestionStaysOpen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	write(t, path, doc)
	write(t, path+".review.yaml", sidecar)

	s := NewServer(&bytes.Buffer{})
	s.setDoc("file://"+path, doc)
	s.closeAppliedSuggestions("file://" + path)

	sc, _ := mrsf.Load(path)
	if c := sc.Find("c1"); c == nil || c.Resolved {
		t.Error("thread must stay open while the original fragment is in the document")
	}
}
