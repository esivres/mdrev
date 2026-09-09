package mrsf

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
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
	if err := sc.save(); err != nil {
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

// Atomicity is what stops a reader from seeing half a review: the file is
// replaced whole or not at all. Asserting only that no temp file is left over
// would pass even for a plain in-place write.
func TestSaveNeverExposesAPartialFile(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(Path(doc), []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	sc, err := Load(doc)
	if err != nil {
		t.Fatal(err)
	}
	// Enough comments that a non-atomic write would have a visible window.
	for i := 0; i < 200; i++ {
		if _, err := sc.Add(Comment{Text: strings.Repeat("padding ", 20), SelectedText: "claim"}); err != nil {
			t.Fatal(err)
		}
	}

	stop := make(chan struct{})
	bad := make(chan error, 1)
	go func() {
		for {
			select {
			case <-stop:
				close(bad)
				return
			default:
			}
			got, err := Load(doc)
			if err != nil {
				select {
				case bad <- err:
				default:
				}
				continue
			}
			if got != nil && len(got.Comments) < 1 {
				select {
				case bad <- errUnexpectedlyEmpty:
				default:
				}
			}
		}
	}()

	for i := 0; i < 50; i++ {
		if err := sc.save(); err != nil {
			t.Fatal(err)
		}
	}
	close(stop)

	if err := <-bad; err != nil {
		t.Errorf("a reader saw an incomplete sidecar: %v", err)
	}
}

var errUnexpectedlyEmpty = errors.New("sidecar read back with no comments")

// A review someone made private must not become world-readable just because a
// comment was added to it.
func TestSaveKeepsFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no Unix permission bits; Go reports 0666 or 0444")
	}
	dir := t.TempDir()
	doc := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(Path(doc), []byte(foreign), 0o600); err != nil {
		t.Fatal(err)
	}
	sc, err := Load(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := sc.save(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(Path(doc))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("permissions after save: got %o, want 600", perm)
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".mrsf-") {
			t.Errorf("temporary file %s left behind", e.Name())
		}
	}
}

// A sidecar kept behind a symlink — a shared review store, a dotfiles setup —
// must keep working: replacing the link with a regular file would fork the
// data and quietly strand every later comment.
func TestSaveWritesThroughASymlink(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "doc.md")
	store := filepath.Join(dir, "store.yaml")
	if err := os.WriteFile(store, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(store, Path(doc)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	sc, err := Load(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sc.Add(Comment{Text: "added", SelectedText: "claim"}); err != nil {
		t.Fatal(err)
	}
	if err := sc.save(); err != nil {
		t.Fatal(err)
	}

	info, err := os.Lstat(Path(doc))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced by a regular file")
	}
	target, err := os.ReadFile(store)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(target), "added") {
		t.Error("the new comment did not reach the file the link points at")
	}
}

// An unknown key that collides with a modelled field panics the encoder, and a
// panic on the write path would take the whole review with it.
func TestCollidingExtraKeyDoesNotPanic(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(Path(doc), []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}
	sc, err := Load(doc)
	if err != nil {
		t.Fatal(err)
	}
	sc.Extra = map[string]any{"document": "collision"}
	sc.Comments[0].Extra = map[string]any{"resolved": "collision"}

	if err := sc.save(); err != nil {
		t.Fatalf("save must not fail on a colliding key: %v", err)
	}
	if _, err := Load(doc); err != nil {
		t.Errorf("the sidecar must still load: %v", err)
	}
}
