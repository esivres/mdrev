package lsp

import (
	"strings"
	"unicode"
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

// anchorWords is how much text before the marker is quoted as the anchor. Long
// enough to be unique in a document, short enough to survive later edits.
const anchorWords = 6

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
	if a := lastWords(text[lineStart(text, start):start], anchorWords); a != "" {
		return a
	}
	return firstWords(text[end:min(end+200, len(text))], anchorWords)
}

func lineStart(text string, offset int) int {
	if i := strings.LastIndexByte(text[:offset], '\n'); i >= 0 {
		return i + 1
	}
	return 0
}

func lastWords(s string, n int) string {
	fields := strings.Fields(s)
	if len(fields) > n {
		fields = fields[len(fields)-n:]
	}
	return trimPunctuation(strings.Join(fields, " "))
}

func firstWords(s string, n int) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	fields := strings.Fields(s)
	if len(fields) > n {
		fields = fields[:n]
	}
	return trimPunctuation(strings.Join(fields, " "))
}

// trimPunctuation keeps the anchor from starting or ending mid-punctuation,
// which reads badly when the comment is listed.
func trimPunctuation(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(",;:", r)
	})
}
