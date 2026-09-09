package mrsf

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode/utf8"
)

// Anchor status values, matching what the reference tool writes.
const (
	ReanchorStatusKey = "x_reanchor_status"
	ReanchorScoreKey  = "x_reanchor_score"
	StatusAnchored    = "anchored"
	StatusOrphaned    = "orphaned"
)

// Orphaned reports that the quoted text is no longer in the document, so the
// recorded position is the last place it was seen rather than where it is.
func (c Comment) Orphaned() bool {
	status, _ := c.Extra[ReanchorStatusKey].(string)
	return status == StatusOrphaned
}

// Reanchor rewrites a comment's position from the document as it is now.
// Without it line and columns describe where the text used to be: they decay
// with every edit above them, and nothing downstream can tell a stale position
// from a current one.
func (s *Sidecar) Reanchor(text string) {
	for i := range s.Comments {
		reanchor(text, &s.Comments[i])
	}
}

func reanchor(text string, c *Comment) {
	if c.SelectedText == "" {
		return // a line-scoped comment has nothing to follow
	}
	if c.SelectedTextHash == "" {
		sum := sha256.Sum256([]byte(c.SelectedText))
		c.SelectedTextHash = hex.EncodeToString(sum[:])
	}

	offset := nearestOccurrence(text, c.SelectedText, c.Line)
	if offset < 0 {
		c.setExtra(ReanchorStatusKey, StatusOrphaned)
		delete(c.Extra, ReanchorScoreKey)
		return
	}

	line, column := position(text, offset)
	endLine, endColumn := position(text, offset+len(c.SelectedText))
	c.Line, c.EndLine = line, endLine
	c.StartColumn, c.EndColumn = &column, &endColumn
	c.setExtra(ReanchorStatusKey, StatusAnchored)
	c.setExtra(ReanchorScoreKey, 1)
}

// nearestOccurrence picks the occurrence closest to the recorded line: a
// fragment can repeat, in prose and again inside a diagram, and the recorded
// line is the only hint of which one the comment meant.
func nearestOccurrence(text, needle string, line int) int {
	best, bestDist := -1, 0
	for off := 0; ; {
		i := strings.Index(text[off:], needle)
		if i < 0 {
			return best
		}
		at := off + i
		found, _ := position(text, at)
		dist := found - line
		if dist < 0 {
			dist = -dist
		}
		if best < 0 || dist < bestDist {
			best, bestDist = at, dist
		}
		off = at + 1
	}
}

// position converts a byte offset into a 1-based line and a 0-based column
// counted in characters, which is what the format records.
func position(text string, offset int) (line, column int) {
	line = 1 + strings.Count(text[:offset], "\n")
	start := strings.LastIndexByte(text[:offset], '\n') + 1
	return line, utf8.RuneCountInString(text[start:offset])
}

func (c *Comment) setExtra(key string, value any) {
	if c.Extra == nil {
		c.Extra = map[string]any{}
	}
	c.Extra[key] = value
}
