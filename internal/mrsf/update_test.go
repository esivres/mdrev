package mrsf

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Two processes writing a review at once is the ordinary case: an agent files
// comments through the CLI while a human resolves them in the editor. Without
// the lock the loser's write is simply gone, with no error anywhere.
func TestConcurrentWritersDoNotLoseComments(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(doc, []byte("Alpha beta gamma.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The lock is what is under test, not how fast it is acquired: under the
	// race detector eight writers take well over the interactive timeout.
	defer func(previous time.Duration) { lockTimeout = previous }(lockTimeout)
	lockTimeout = time.Minute

	const writers, each = 8, 10
	var wg sync.WaitGroup
	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < each; i++ {
				err := Update(doc, func(sc *Sidecar) error {
					_, err := sc.Add(Comment{
						Text:         fmt.Sprintf("writer %d comment %d", w, i),
						SelectedText: "beta",
					})
					return err
				})
				if err != nil {
					t.Errorf("writer %d: %v", w, err)
					return
				}
			}
		}(w)
	}
	wg.Wait()

	sc, err := Load(doc)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(sc.Comments); got != writers*each {
		t.Errorf("comments after %d concurrent writes: got %d, want %d",
			writers*each, got, writers*each)
	}
}

// The lock lives beside the sidecar, not on it: Save replaces the sidecar by
// rename, so a lock held on that file would be orphaned by the first write and
// two writers would each hold an exclusive lock on a different file.
func TestLockFileIsSeparateFromTheSidecar(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "doc.md")
	if err := Update(doc, func(sc *Sidecar) error {
		_, err := sc.Add(Comment{Text: "hello"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(Path(doc) + ".lock"); err != nil {
		t.Errorf("the lock file must sit beside the sidecar: %v", err)
	}
}
