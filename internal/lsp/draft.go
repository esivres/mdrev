package lsp

import (
	"strings"

	"github.com/esivres/mdrev/internal/anchor"
)

// Comments are typed into the document as CriticMarkup, {>>like this<<}. LSP
// cannot prompt for text, and Zed implements neither window/showDocument nor
// workspace/applyEdit, so the document is the only input surface available.
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
			// Its code action deletes whatever the marker covers.
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

// A comment may span lines but not a blank line or another marker: past
// either, the opener was stray, and pairing it with a distant closer would
// swallow prose that filing then deletes.
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

// Not a search for "\n\n": on a CRLF document that is "\r\n\r\n", and the
// guard would silently do nothing on every file written on Windows.
func hasBlankLine(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimRight(line, "\r") == "" {
			return true
		}
	}
	return false
}

// A remark goes after what it is about; failing that, before.
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
