package mrsf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const foreign = `mrsf_version: "1.0"
document: doc.md
reviewer: alice
review_status: in_progress
comments:
  - id: c1
    author: Alice
    text: Needs a source.
    resolved: false
    line: 3
    selected_text: the claim
    tags: [editorial]
`

// A sidecar is shared with other MRSF tools. Dropping keys we do not model
// would silently delete their data every time a comment is added here.
func TestForeignKeysSurviveASave(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(Path(doc), []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}

	sc, err := Load(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sc.Add(Comment{Text: "second", SelectedText: "claim"}); err != nil {
		t.Fatal(err)
	}
	if err := sc.Save(); err != nil {
		t.Fatal(err)
	}

	saved, err := os.ReadFile(Path(doc))
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"reviewer: alice", "review_status: in_progress", "tags:"} {
		if !strings.Contains(string(saved), key) {
			t.Errorf("%q was dropped on save:\n%s", key, saved)
		}
	}
}

// A crash or a full disk must not leave a truncated review behind: a partial
// YAML file can still parse, and would look like comments simply vanished.
func TestSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(Path(doc), []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	sc, err := Load(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := sc.Save(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".mrsf-") {
			t.Errorf("temporary file %s left behind", e.Name())
		}
	}
	info, err := os.Stat(Path(doc))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("permissions after save: got %o, want 644", perm)
	}
}
