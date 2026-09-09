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

	var reply, parent *mrsf.Comment
	if err := mrsf.Update(*file, func(sc *mrsf.Sidecar) error {
		var err error
		parent, err = findByPrefix(sc, *id)
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
		// Resolve before adding: Add appends, which may move the backing array
		// and leave the parent pointer aiming at the old one.
		parentID, parentLine, parentQuote := parent.ID, parent.Line, parent.SelectedText
		if *resolve {
			parent.Resolved = true
			parent.SetOutcome(mrsf.OutcomeResolved)
		}
		reply, err = sc.Add(mrsf.Comment{
			Author:       firstNonEmpty(*author, mrsf.DefaultAuthor()),
			Text:         *text,
			Line:         parentLine,
			SelectedText: parentQuote,
			ReplyTo:      parentID,
		})
		return err
	}); err != nil {
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
