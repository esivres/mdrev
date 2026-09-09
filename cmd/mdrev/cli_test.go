package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// build compiles the CLI once per test binary; the commands are worth testing
// end to end because their failures so far have been silent — a flag accepted
// and ignored, a flag applied to a stale copy.
func build(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "mdrev")
	out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return bin
}

func run(t *testing.T, bin, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func fixture(t *testing.T) (bin, dir string) {
	t.Helper()
	bin = build(t)
	dir = t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "doc.md"),
		[]byte("Alpha beta gamma.\n\nDelta epsilon.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return bin, dir
}

// --resolve is the only way an agent can close a thread, and it used to be
// dropped silently because Add reallocated the slice the parent pointed into.
func TestReplyResolvesTheParent(t *testing.T) {
	bin, dir := fixture(t)
	if out, err := run(t, bin, dir, "comment", "--file", "doc.md",
		"--quote", "beta", "--line", "1", "--text", "why?", "--author", "A"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	id := firstID(t, bin, dir)

	if out, err := run(t, bin, dir, "reply", "--file", "doc.md",
		"--id", id[:8], "--text", "because", "--author", "B", "--resolve"); err != nil {
		t.Fatalf("%v: %s", err, out)
	}

	out, err := run(t, bin, dir, "list", "doc.md", "--json")
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if strings.Contains(out, id) {
		t.Errorf("the thread should be resolved and gone from the open list:\n%s", out)
	}
}

// The JSON is the machine interface: an agent looks up the sidecar's field
// names, and needs the human's replies nested in the thread it answered.
func TestListJSONUsesSidecarFieldNamesAndCarriesReplies(t *testing.T) {
	bin, dir := fixture(t)
	run(t, bin, dir, "comment", "--file", "doc.md", "--quote", "beta", "--line", "1",
		"--type", "suggestion", "--suggest", "BETA", "--text", "shout it", "--author", "A")
	id := firstID(t, bin, dir)
	run(t, bin, dir, "reply", "--file", "doc.md", "--id", id[:8], "--text", "agreed", "--author", "B")

	out, err := run(t, bin, dir, "list", "doc.md", "--json")
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var got []struct {
		ID           string `json:"id"`
		SelectedText string `json:"selected_text"`
		Suggested    string `json:"x_suggested_text"`
		Replies      []struct {
			Text string `json:"text"`
		} `json:"replies"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not the documented JSON: %v\n%s", err, out)
	}
	if len(got) != 1 {
		t.Fatalf("want one thread, got %d", len(got))
	}
	if got[0].SelectedText != "beta" {
		t.Errorf("selected_text: got %q", got[0].SelectedText)
	}
	if got[0].Suggested != "BETA" {
		t.Errorf("x_suggested_text must be exposed, got %q", got[0].Suggested)
	}
	if len(got[0].Replies) != 1 || got[0].Replies[0].Text != "agreed" {
		t.Errorf("replies must be nested in their thread, got %+v", got[0].Replies)
	}
}

// A clean review must be an empty array rather than null, or an agent parsing
// it has to special-case the happy path.
func TestListJSONIsAnArrayWhenEmpty(t *testing.T) {
	bin, dir := fixture(t)
	out, err := run(t, bin, dir, "list", "doc.md", "--json")
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("want [], got %q", out)
	}
}

// A typo in a path used to look exactly like a document with nothing to
// review, which is the worst possible answer for a script.
func TestMissingDocumentIsAnError(t *testing.T) {
	bin, dir := fixture(t)
	out, err := run(t, bin, dir, "list", "nope.md")
	if err == nil {
		t.Errorf("want a non-zero exit, got success: %s", out)
	}
	if !strings.Contains(out, "nope.md") {
		t.Errorf("the message must name the file, got %q", out)
	}
}

// Go's flag package stops at the first positional argument, so a flag written
// after the file used to be accepted and ignored.
func TestFlagsAfterThePathAreHonoured(t *testing.T) {
	bin, dir := fixture(t)
	run(t, bin, dir, "comment", "--file", "doc.md", "--quote", "beta", "--line", "1", "--text", "x")

	out, err := run(t, bin, dir, "list", "doc.md", "--json")
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "[") {
		t.Errorf("--json after the path must apply, got:\n%s", out)
	}
}

func firstID(t *testing.T, bin, dir string) string {
	t.Helper()
	out, err := run(t, bin, dir, "list", "doc.md", "--json")
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	var got []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil || len(got) == 0 {
		t.Fatalf("no comment found: %v\n%s", err, out)
	}
	return got[0].ID
}
