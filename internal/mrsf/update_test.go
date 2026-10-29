package mrsf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// Re-anchoring the whole review on every save is what made saving one comment
// on a large review take minutes: every comment rescans the document. When the
// document is byte-for-byte the text the comments were anchored against, that
// work cannot change a single position, so the save must not redo it. The
// recorded score is the observable: a re-anchoring pass always rewrites it.
func TestUnchangedDocumentIsNotReanchored(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(doc, []byte("Alpha beta gamma.\n\nDelta epsilon zeta.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Update(doc, func(sc *Sidecar) error {
		_, err := sc.Add(Comment{Text: "here", Line: 1, SelectedText: "Alpha beta"})
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Rewrite the score the way only a foreign tool would, to a value no
	// re-anchoring pass would ever produce.
	path := Path(doc)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var edited []string
	for _, ln := range strings.Split(string(data), "\n") {
		if strings.Contains(ln, ReanchorScoreKey) {
			if at := strings.Index(ln, ": "); at >= 0 {
				ln = ln[:at+2] + "0.42"
			}
		}
		edited = append(edited, ln)
	}
	if err := os.WriteFile(path, []byte(strings.Join(edited, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Update(doc, func(sc *Sidecar) error {
		_, err := sc.Add(Comment{Text: "one more"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	sc, err := Load(doc)
	if err != nil {
		t.Fatal(err)
	}
	if got := sc.Comments[0].Extra[ReanchorScoreKey]; got != 0.42 {
		t.Errorf("a save against an unchanged document must not re-anchor; score rewritten to %v", got)
	}

	// An edit above the comment decays the recorded position; the next save
	// must notice the document changed and re-anchor for real.
	if err := os.WriteFile(doc,
		[]byte("A preface settles in.\n\nAlpha beta gamma.\n\nDelta epsilon zeta.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Update(doc, func(sc *Sidecar) error {
		_, err := sc.Add(Comment{Text: "and another"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	sc, err = Load(doc)
	if err != nil {
		t.Fatal(err)
	}
	first := sc.Comments[0]
	if first.Line != 3 {
		t.Errorf("after an edit above it the comment must follow its text: line %d, want 3", first.Line)
	}
	if got := first.Extra[ReanchorScoreKey]; got == 0.42 {
		t.Error("the save after an edit must re-run the anchoring and rewrite the score")
	}
}

// A review collected over a long document with every quoted line edited —
// the state of a document under active revision. One save must re-anchor the
// whole review, finding every edited line, without the per-comment
// full-document scan that costs minutes at this scale.
func TestEditedLargeReviewReanchorsInOneSave(t *testing.T) {
	const docLines, nComments = 300, 60
	var before, after strings.Builder
	for i := range docLines {
		fmt.Fprintf(&before, "topic%05d alpha bravo charlie delta echo foxtrot golf hotel\n", i)
		fmt.Fprintf(&after, "edited%05d alpha bravo charlie delta echo foxtrot golf hotel\n", i)
	}
	dir := t.TempDir()
	doc := filepath.Join(dir, "doc.md")
	if err := os.WriteFile(doc, []byte(before.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Update(doc, func(sc *Sidecar) error {
		for i := range nComments {
			idx := i * docLines / nComments
			quote := fmt.Sprintf("topic%05d alpha bravo charlie", idx)
			_, err := sc.Add(Comment{Text: "remark", Line: idx + 1, SelectedText: quote})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(doc, []byte(after.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := Update(doc, func(sc *Sidecar) error {
		_, err := sc.Add(Comment{Text: "one more"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)

	sc, err := Load(doc)
	if err != nil {
		t.Fatal(err)
	}
	for i := range nComments {
		want := i*docLines/nComments + 1
		c := sc.Comments[i]
		if c.Orphaned() {
			t.Fatalf("comment %d (line %d): an edited line must keep its anchor", i, want)
		}
		if c.Line != want {
			t.Errorf("comment %d: line %d, want %d", i, c.Line, want)
		}
	}
	if elapsed > 5*time.Second {
		t.Errorf("one save over an edited %d-line review with %d comments took %v; "+
			"the re-anchor must not rescan the document per comment", docLines, nComments, elapsed)
	}
}
