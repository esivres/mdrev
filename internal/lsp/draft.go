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
		end, ok := draftEnd(text, start)
		if !ok {
			// An unterminated marker must not swallow the rest of the file: its
			// code action deletes the range it covers.
			off = start + len(draftOpen)
			continue
		}

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

// draftEnd finds the marker's closing tag. A comment may span lines, but not a
// blank line and not another marker: past either, the opener was a stray one
// and pairing it with a distant closer would put unrelated prose inside the
// comment — and delete it from the document when the comment is filed.
func draftEnd(text string, start int) (int, bool) {
	rest := text[start+len(draftOpen):]
	closing := strings.Index(rest, draftClose)
	if closing < 0 {
		return 0, false
	}
	body := rest[:closing]
	if hasBlankLine(body) || strings.Contains(body, draftOpen) {
		return 0, false
	}
	return start + len(draftOpen) + closing + len(draftClose), true
}

// hasBlankLine reports whether the text contains an empty line. It cannot be a
// search for "\n\n": on a CRLF document that is "\r\n\r\n", and the guard
// would quietly do nothing on every file written by a Windows editor.
func hasBlankLine(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimRight(line, "\r") == "" {
			return true
		}
	}
	return false
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
