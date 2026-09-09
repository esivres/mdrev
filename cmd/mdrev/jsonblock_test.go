package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const handWritten = `// my own bindings, hands off
[
  {
    "context": "Editor",
    "bindings": {
      "ctrl-g": "go_to_line::Toggle" // I use this constantly
    }
  }
]
`

// The keymap belongs to the user and is usually long, hand-edited and full of
// comments. Rewriting it as parsed JSON would silently drop all of that, so our
// entries go in as a marked block and everything else stays byte for byte.
func TestMergeKeepsTheRestOfTheFileVerbatim(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keymap.json")
	write(t, path, handWritten)

	if err := mergeBlock(path, `{"context": "Editor", "bindings": {"alt-c": "x"}}`); err != nil {
		t.Fatal(err)
	}

	got := read(t, path)
	for _, want := range []string{
		"// my own bindings, hands off",
		`"ctrl-g": "go_to_line::Toggle" // I use this constantly`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("lost %q:\n%s", want, got)
		}
	}
	if !strings.Contains(got, `"alt-c"`) {
		t.Errorf("our binding was not added:\n%s", got)
	}
}

// setup is run again after an upgrade or a change of mind; a second run must
// replace our block rather than stack another copy of it.
func TestMergeReplacesItsOwnBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keymap.json")
	write(t, path, handWritten)

	if err := mergeBlock(path, `{"bindings": {"alt-c": "first"}}`); err != nil {
		t.Fatal(err)
	}
	if err := mergeBlock(path, `{"bindings": {"ctrl-alt-k": "second"}}`); err != nil {
		t.Fatal(err)
	}

	got := read(t, path)
	if strings.Count(got, blockBegin) != 1 {
		t.Errorf("block appears %d times:\n%s", strings.Count(got, blockBegin), got)
	}
	if strings.Contains(got, "first") {
		t.Error("the previous block was left behind")
	}
	if !strings.Contains(got, "ctrl-alt-k") {
		t.Errorf("the new block is missing:\n%s", got)
	}
}

func TestMergeCreatesAFileThatIsNotThere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "tasks.json")
	if err := mergeBlock(path, `{"label": "x"}`); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); !strings.HasPrefix(strings.TrimSpace(got), "[") {
		t.Errorf("want a JSON array, got:\n%s", got)
	}
}

// Warning about a clash is the whole point of asking, so it must see the user's
// bindings and ignore ours.
func TestBoundToFindsTheUsersBindingAndIgnoresOurs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "keymap.json")
	write(t, path, handWritten)
	if err := mergeBlock(path, `{"bindings": {"alt-c": "task::Spawn"}}`); err != nil {
		t.Fatal(err)
	}

	if got := boundTo(path, "ctrl-g"); got != "go_to_line::Toggle" {
		t.Errorf("user binding: got %q, want go_to_line::Toggle", got)
	}
	if got := boundTo(path, "alt-c"); got != "" {
		t.Errorf("our own binding must not count as a conflict, got %q", got)
	}
	if got := boundTo(path, "alt-shift-q"); got != "" {
		t.Errorf("unbound key must report nothing, got %q", got)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// An earlier version wrote these files whole, without markers. Merging beside
// what it left would give the editor two of every task, so setup has to be able
// to see the old entries.
func TestStaleEntriesAreFoundOutsideOurBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	write(t, path, `[
  { "label": "Comment on selection", "command": "mdrev" }
]
`)
	if got := staleEntries(path, []string{"Comment on selection", "Review threads"}); len(got) != 1 || got[0] != "Comment on selection" {
		t.Errorf("want the old entry reported, got %v", got)
	}

	if err := mergeBlock(path, `{"label": "Review threads"}`); err != nil {
		t.Fatal(err)
	}
	if got := staleEntries(path, []string{"Review threads"}); len(got) != 0 {
		t.Errorf("an entry inside our block is not stale, got %v", got)
	}
}

// An earlier version wrote these files whole. Merging beside what it left would
// give the editor two of every task, and a warning is no help — the file is
// already wrong by the time anyone reads it.
func TestOldEntriesAreRemovedNotDuplicated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	write(t, path, `// mine, keep it
[
  { "label": "my own task", "command": "make" },
  { "label": "Comment on selection", "command": "/old/path/mdrev" },
  { "label": "Review threads", "command": "/old/path/mdrev" }
]
`)

	err := mergeBlock(path, `{"label": "Comment on selection", "command": "/new/mdrev"}`,
		"Comment on selection", "Review threads")
	if err != nil {
		t.Fatal(err)
	}

	got := read(t, path)
	if n := strings.Count(got, `"Comment on selection"`); n != 1 {
		t.Errorf("task appears %d times, want 1:\n%s", n, got)
	}
	if strings.Contains(got, "/old/path/mdrev") {
		t.Errorf("the old entry survived:\n%s", got)
	}
	if !strings.Contains(got, `"my own task"`) || !strings.Contains(got, "// mine, keep it") {
		t.Errorf("the user's own entries must survive:\n%s", got)
	}
	if strings.Contains(got, `"Review threads"`) {
		t.Error("an entry we no longer write must not be left behind")
	}
}

// Removal must understand strings and comments, or a brace inside either would
// cut an object in the wrong place.
func TestRemovalIsNotConfusedByBracesInText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	write(t, path, `[
  { "label": "mine", "command": "echo '{ not a real brace }'" }, // } neither is this
  { "label": "Comment on selection", "command": "mdrev" }
]
`)

	if err := mergeBlock(path, `{"label": "Comment on selection"}`, "Comment on selection"); err != nil {
		t.Fatal(err)
	}

	got := read(t, path)
	if !strings.Contains(got, `"echo '{ not a real brace }'"`) {
		t.Errorf("an entry with braces in a string was damaged:\n%s", got)
	}
	if n := strings.Count(got, `"Comment on selection"`); n != 1 {
		t.Errorf("task appears %d times, want 1:\n%s", n, got)
	}
}
