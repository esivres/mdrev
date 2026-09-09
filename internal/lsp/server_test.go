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

// Zed applies a code action's edit and returns without running its command, so
// an action that must both write and edit has to do the write when the client
// resolves it. Getting this wrong deleted the comment from the document and
// recorded nothing.
func TestFilingADraftWritesAndEditsOnResolve(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	text := "The service accepts raw input. {>>whose input?<<}\n"
	write(t, path, text)
	uri := "file://" + path

	s := NewServer(&bytes.Buffer{})
	s.setDoc(uri, text)

	params, _ := json.Marshal(map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 0, "character": 33},
			"end":   map[string]any{"line": 0, "character": 33},
		},
	})
	actions := s.codeActions(params)
	if len(actions) != 1 {
		t.Fatalf("want the filing action, got %+v", actions)
	}
	if actions[0].Edit != nil || actions[0].Command != nil {
		t.Error("the action must carry neither an edit nor a command before resolve")
	}

	raw, _ := json.Marshal(actions[0])
	resolved, ok := s.resolveCodeAction(raw).(CodeAction)
	if !ok || resolved.Edit == nil {
		t.Fatalf("resolve must return the edit, got %+v", resolved)
	}

	sc, err := mrsf.Load(path)
	if err != nil || sc == nil {
		t.Fatalf("the comment must be recorded: %v", err)
	}
	if len(sc.Comments) != 1 || sc.Comments[0].Text != "whose input?" {
		t.Errorf("recorded comment: %+v", sc.Comments)
	}
	if edits := resolved.Edit.Changes[uri]; len(edits) != 1 || edits[0].NewText != "" {
		t.Errorf("the edit must remove the marker, got %+v", edits)
	}
}

// If the write fails there must be no edit: the marker stays on screen instead
// of being deleted along with the comment it carried.
func TestFailedFilingLeavesTheDocumentAlone(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.md")
	text := "Some prose. {>>note<<}\n"
	write(t, path, text)
	uri := "file://" + path

	s := NewServer(&bytes.Buffer{})
	s.setDoc(uri, text)
	write(t, path+".review.yaml", "this: is: not: valid: yaml\n")

	params, _ := json.Marshal(map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range": map[string]any{
			"start": map[string]any{"line": 0, "character": 14},
			"end":   map[string]any{"line": 0, "character": 14},
		},
	})
	actions := s.codeActions(params)
	if len(actions) == 0 {
		t.Fatal("expected the filing action")
	}
	raw, _ := json.Marshal(actions[0])
	resolved := s.resolveCodeAction(raw).(CodeAction)

	if resolved.Edit != nil {
		t.Error("a failed write must not delete the marker from the document")
	}
}
