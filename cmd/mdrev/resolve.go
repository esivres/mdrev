package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/esivres/mdrev/internal/mrsf"
)

// resolveComment closes a thread without replying, recording how it ended.
func resolveComment(args []string) error {
	fs := flag.NewFlagSet("resolve", flag.ExitOnError)
	file := fs.String("file", "", "path to the document")
	id := fs.String("id", "", "comment to close; an id prefix is enough")
	dismiss := fs.Bool("dismiss", false, "record the thread as turned down rather than settled")
	reopen := fs.Bool("reopen", false, "reopen a closed thread instead")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return fmt.Errorf("unexpected argument %q; the document is given with --file", rest[0])
	}
	if *file == "" || *id == "" {
		return fmt.Errorf("--file and --id are required")
	}
	if err := requireDocument(*file); err != nil {
		return err
	}

	var closed *mrsf.Comment
	if err := mrsf.Update(*file, func(sc *mrsf.Sidecar) error {
		c, err := findByPrefix(sc, *id)
		if err != nil {
			return err
		}
		if *reopen {
			c.Resolved = false
			c.SetOutcome("")
		} else {
			c.Resolved = true
			outcome := mrsf.OutcomeResolved
			if *dismiss {
				outcome = mrsf.OutcomeDismissed
			}
			c.SetOutcome(outcome)
		}
		closed = c
		return nil
	}); err != nil {
		return err
	}
	fmt.Printf("%s %s\n", map[bool]string{true: "Reopened", false: "Closed"}[*reopen], shortID(closed.ID))
	return nil
}

// applySuggestion is the one place this tool writes to the document. Without
// it an accepted suggestion could only be landed from inside an editor.
func applySuggestion(args []string) error {
	fs := flag.NewFlagSet("apply", flag.ExitOnError)
	file := fs.String("file", "", "path to the document")
	id := fs.String("id", "", "comment whose suggestion to apply; an id prefix is enough")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return fmt.Errorf("unexpected argument %q; the document is given with --file", rest[0])
	}
	if *file == "" || *id == "" {
		return fmt.Errorf("--file and --id are required")
	}
	if err := requireDocument(*file); err != nil {
		return err
	}

	var applied *mrsf.Comment
	var at int
	// Both under one lock: a crash between them would leave a suggestion
	// applied but still open, or closed but not applied.
	if err := mrsf.Update(*file, func(sc *mrsf.Sidecar) error {
		c, err := findByPrefix(sc, *id)
		if err != nil {
			return err
		}
		suggested, ok := c.SuggestedText()
		if !ok {
			return fmt.Errorf("comment %s proposes no replacement", shortID(c.ID))
		}
		if c.SelectedText == "" {
			return fmt.Errorf("comment %s has no anchored text to replace", shortID(c.ID))
		}

		data, err := os.ReadFile(*file)
		if err != nil {
			return err
		}
		// The fragment may appear elsewhere; rewriting the wrong one is
		// silent damage. A quote can span lines, so occurrences are matched
		// against the whole text and ranked by their starting line — a
		// per-line scan would never find a multi-line fragment at all.
		text := string(data)
		off, found := nearestOffset(text, c.SelectedText, c.Line)
		if !found {
			return fmt.Errorf("the text %q is no longer in %s", c.SelectedText, *file)
		}
		at = 1 + strings.Count(text[:off], "\n") // for the report below
		text = text[:off] + suggested + text[off+len(c.SelectedText):]

		if err := writeFileAtomically(*file, []byte(text)); err != nil {
			return err
		}
		c.Resolved = true
		c.SetOutcome(mrsf.OutcomeApplied)
		applied = c
		return nil
	}); err != nil {
		return err
	}
	fmt.Printf("Applied %s at %s:%d\n", shortID(applied.ID), *file, at)
	return nil
}

// nearestOffset finds the occurrence of needle closest to the given 1-based
// line, as a byte offset into text. Ties keep the earlier offset, the way the
// re-anchorer picks.
func nearestOffset(text, needle string, line int) (int, bool) {
	best, bestDist := -1, 1<<30
	// Occurrences arrive in increasing offset order, so newlines are counted
	// incrementally over the slice since the previous match — rescanning the
	// prefix for every occurrence would make apply O(k·n) on a large file.
	newlines, off := 0, 0
	for off < len(text) {
		i := strings.Index(text[off:], needle)
		if i < 0 {
			break
		}
		at := off + i
		newlines += strings.Count(text[off:at], "\n")
		d := 1 + newlines - line
		if d < 0 {
			d = -d
		}
		if d < bestDist {
			best, bestDist = at, d
		}
		off = at + 1
	}
	return best, best >= 0
}

// Replaces a file through a temporary one beside it, keeping its mode.
func writeFileAtomically(path string, data []byte) error {
	target := path
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	perm := os.FileMode(0o644)
	if info, err := os.Stat(target); err == nil {
		perm = info.Mode().Perm()
	}

	tmp, err := os.CreateTemp(filepath.Dir(target), ".mdrev-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), perm); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), target)
}
