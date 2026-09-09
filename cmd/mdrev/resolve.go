package main

import (
	"flag"
	"fmt"
	"os"
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
	if _, err := parseFlags(fs, args); err != nil {
		return err
	}
	if *file == "" || *id == "" {
		return fmt.Errorf("--file and --id are required")
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
	if _, err := parseFlags(fs, args); err != nil {
		return err
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

	if err := os.WriteFile(*file, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
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
