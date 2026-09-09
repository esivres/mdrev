package main

import (
	"fmt"

	"github.com/esivres/mdrev/internal/mrsf"
	"github.com/esivres/mdrev/internal/tui"
)

// offerReply asks whether to answer a discussion already on this line, and
// carries it out. Reports whether anything was done, so the caller can fall
// through to writing a new comment.
func offerReply(document string, line int, author string) (bool, error) {
	if line == 0 {
		return false, nil
	}
	sidecar, err := mrsf.Load(document)
	if err != nil || sidecar == nil {
		return false, err
	}

	var here []mrsf.Thread
	for _, t := range sidecar.Threads(false) {
		if t.Parent.Line == line {
			here = append(here, t)
		}
	}
	if len(here) == 0 {
		return false, nil
	}

	choices := make([]tui.Choice, 0, len(here)+1)
	for _, t := range here {
		label := fmt.Sprintf("Reply to %s", t.Parent.Author)
		if n := len(t.Replies); n > 0 {
			label += fmt.Sprintf(" (%d replies)", n)
		}
		choices = append(choices, tui.Choice{Label: label, Detail: t.Parent.Text})
	}
	choices = append(choices, tui.Choice{
		Label:  "Write a new comment on this line",
		Detail: "",
	})

	at, ok, err := tui.Pick("This line is already under discussion", choices)
	if err != nil || !ok {
		return ok, err
	}
	if at == len(here) {
		return false, nil // write a new one instead
	}

	parent := here[at].Parent
	draft, ok, err := tui.Compose(parent.SelectedText, parent.Line)
	if err != nil || !ok || draft.Text == "" {
		return true, err
	}

	if err := mrsf.Update(document, func(sc *mrsf.Sidecar) error {
		if sc.Find(parent.ID) == nil {
			return fmt.Errorf("that thread is no longer in the review")
		}
		_, addErr := sc.Add(mrsf.Comment{
			Author:       firstNonEmpty(author, mrsf.DefaultAuthor()),
			Text:         draft.Text,
			Line:         parent.Line,
			SelectedText: parent.SelectedText,
			ReplyTo:      parent.ID,
		})
		return addErr
	}); err != nil {
		return true, err
	}

	fmt.Printf("Replied to %s\n", shortID(parent.ID))
	return true, nil
}
