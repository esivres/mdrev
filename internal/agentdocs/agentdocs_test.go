package agentdocs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// AGENTS.md belongs to the user; rewriting the whole file to add our section
// would destroy whatever else they keep there.
func TestAppendKeepsExistingContent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	existing := "# House rules\n\nRun the tests before pushing.\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := AppendToAgentsFile(path); err != nil {
		t.Fatal(err)
	}

	got := read(t, path)
	if !strings.HasPrefix(got, existing) {
		t.Errorf("existing content must survive verbatim, got:\n%s", got)
	}
	if !strings.Contains(got, heading) {
		t.Error("our section was not added")
	}
}

// init is run more than once — after an upgrade, or in a directory someone
// else set up — and each run must not stack another copy of the same text.
func TestAppendIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := AppendToAgentsFile(path); err != nil {
		t.Fatal(err)
	}
	first := read(t, path)
	if err := AppendToAgentsFile(path); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != first {
		t.Errorf("a second run changed the file:\n%s", got)
	}
	if n := strings.Count(read(t, path), heading); n != 1 {
		t.Errorf("heading appears %d times, want 1", n)
	}
}

// A skill the user has edited must not be silently replaced by ours.
func TestSkillIsNotOverwritten(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "skills", "mdrev")
	if err := WriteSkill(dir); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(path, []byte("edited by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteSkill(dir); err != nil {
		t.Fatal(err)
	}
	if got := read(t, path); got != "edited by hand\n" {
		t.Errorf("an existing skill must be left alone, got:\n%s", got)
	}
}

// The skill is loaded by name and description, so a missing front matter would
// make it invisible rather than merely ugly.
func TestSkillCarriesFrontMatter(t *testing.T) {
	dir := t.TempDir()
	if err := WriteSkill(dir); err != nil {
		t.Fatal(err)
	}
	got := read(t, filepath.Join(dir, "SKILL.md"))
	for _, want := range []string{"---\nname: mdrev\n", "description: "} {
		if !strings.Contains(got, want) {
			t.Errorf("front matter is missing %q:\n%s", want, got[:min(200, len(got))])
		}
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
