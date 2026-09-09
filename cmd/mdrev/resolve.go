package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/esivres/mdrev/internal/anchor"
	"github.com/esivres/mdrev/internal/mrsf"
)

// resolveComment closes a thread without replying to it, recording how it
// ended. Applying a suggestion and turning it down otherwise leave identical
// state, and an agent that cannot tell them apart proposes the same edit again.
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

	sidecar, err := mrsf.Load(*file)
	if err != nil {
		return err
	}
	if sidecar == nil {
		return fmt.Errorf("%s has no review sidecar", *file)
	}
	c, err := findByPrefix(sidecar, *id)
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
	if err := sidecar.Save(); err != nil {
		return err
	}
	fmt.Printf("%s %s\n", map[bool]string{true: "Reopened", false: "Closed"}[*reopen], shortID(c.ID))
	return nil
}

// applySuggestion is the one place this tool writes to the document itself.
// Without it an accepted suggestion can only be landed from inside an editor,
// which leaves an agent unable to finish the work it proposed.
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

	sidecar, err := mrsf.Load(*file)
	if err != nil {
		return err
	}
	if sidecar == nil {
		return fmt.Errorf("%s has no review sidecar", *file)
	}
	c, err := findByPrefix(sidecar, *id)
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
	lines := strings.Split(string(data), "\n")
	if !anchor.Found(lines, c.SelectedText) {
		return fmt.Errorf("the text %q is no longer in %s", c.SelectedText, *file)
	}
	// Replace the occurrence nearest the comment: the fragment may well appear
	// elsewhere, and rewriting the wrong one would be silent damage.
	at := anchor.NearestLine(lines, c.SelectedText, c.Line-1)
	lines[at] = strings.Replace(lines[at], c.SelectedText, suggested, 1)

	// This is the user's document, so it is replaced whole or not at all.
	if err := writeFileAtomically(*file, []byte(strings.Join(lines, "\n"))); err != nil {
		return err
	}
	c.Resolved = true
	c.SetOutcome(mrsf.OutcomeApplied)
	if err := sidecar.Save(); err != nil {
		return err
	}
	fmt.Printf("Applied %s at %s:%d\n", shortID(c.ID), *file, at+1)
	return nil
}

// writeFileAtomically replaces a file through a temporary one in the same
// directory, keeping the mode it already had.
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
	defer os.Remove(tmp.Name())

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
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
