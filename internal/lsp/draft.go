package lsp

import (
	"strings"

	"github.com/esivres/mdrev/internal/anchor"
)

// Comments can be typed straight into the document using CriticMarkup's
// comment syntax, {>>like this<<}. LSP has no way to prompt a human for text,
// and Zed supports neither window/showDocument nor workspace/applyEdit, so the
// document itself is the only input surface a language server can offer.
const (
	draftOpen  = "{>>"
	draftClose = "<<}"
)

type draft struct {
	Text   string // what the human typed
	Anchor string // the fragment the comment is about
	Start  int    // byte offset of the marker
	End    int
}

func findDrafts(text string) []draft {
	var out []draft
	for off := 0; ; {
		i := strings.Index(text[off:], draftOpen)
		if i < 0 {
			return out
		}
		start := off + i
		j := strings.Index(text[start:], draftClose)
		if j < 0 {
			return out
		}
		end := start + j + len(draftClose)

		body := strings.TrimSpace(text[start+len(draftOpen) : end-len(draftClose)])
		if body != "" {
			out = append(out, draft{
				Text:   body,
				Anchor: anchorFor(text, start, end),
				Start:  start,
				End:    end,
			})
		}
		off = end
	}
}

// anchorFor quotes the words just before the marker, which is where a reader
// naturally puts a remark. Failing that — the marker opens a line — it quotes
// the words just after it.
func anchorFor(text string, start, end int) string {
	if a := anchor.Before(text[lineStart(text, start):start]); a != "" {
		return a
	}
	return anchor.After(text[end:min(end+200, len(text))])
}

func lineStart(text string, offset int) int {
	if i := strings.LastIndexByte(text[:offset], '\n'); i >= 0 {
		return i + 1
	}
	return 0
}
