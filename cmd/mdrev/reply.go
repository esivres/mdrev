package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/esivres/mdrev/internal/mrsf"
)

// replyToComment adds a threaded reply. MRSF models a thread as comments
// pointing at their parent, so a reply inherits the parent's anchor and never
// needs one of its own.
func replyToComment(args []string) error {
	fs := flag.NewFlagSet("reply", flag.ExitOnError)
	file := fs.String("file", "", "path to the document")
	id := fs.String("id", "", "id of the comment to reply to (prefix is enough)")
	text := fs.String("text", "", "reply text; read from stdin when empty")
	author := fs.String("author", "", "reply author")
	useEditor := fs.Bool("editor", false, "compose the reply in $EDITOR instead of stdin")
	resolve := fs.Bool("resolve", false, "mark the thread resolved after replying")
	if err := fs.Parse(args); err != nil {
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
	parent, err := findByPrefix(sidecar, *id)
	if err != nil {
		return err
	}

	if *text == "" {
		body, err := readText(parent.SelectedText, parent.Line, *useEditor)
		if err != nil {
			return err
		}
		*text = body
	}
	if *text == "" {
		return fmt.Errorf("reply text is empty")
	}

	reply, err := sidecar.Add(mrsf.Comment{
		Author:       cmp(*author, gitUserName()),
		Text:         *text,
		Line:         parent.Line,
		SelectedText: parent.SelectedText,
		ReplyTo:      parent.ID,
	})
	if err != nil {
		return err
	}
	if *resolve {
		parent.Resolved = true
	}
	if err := sidecar.Save(); err != nil {
		return err
	}
	fmt.Printf("Replied %s to %s\n", shortID(reply.ID), shortID(parent.ID))
	return nil
}

func findByPrefix(s *mrsf.Sidecar, prefix string) (*mrsf.Comment, error) {
	var found *mrsf.Comment
	for i := range s.Comments {
		if strings.HasPrefix(s.Comments[i].ID, prefix) {
			if found != nil {
				return nil, fmt.Errorf("id %q is ambiguous", prefix)
			}
			found = &s.Comments[i]
		}
	}
	if found == nil {
		return nil, fmt.Errorf("no comment with id %q", prefix)
	}
	return found, nil
}
