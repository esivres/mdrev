package lsp

import (
	"strings"
	"unicode/utf16"

	"github.com/esivres/mdrev/internal/mrsf"
)

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// lineIndex maps byte offsets to LSP positions. LSP counts characters in UTF-16
// code units, not bytes or runes.
type lineIndex struct {
	starts []int // byte offset of each line start
	text   string
}

func newLineIndex(text string) *lineIndex {
	starts := []int{0}
	for i, r := range text {
		if r == '\n' {
			starts = append(starts, i+1)
		}
	}
	return &lineIndex{starts: starts, text: text}
}

func (li *lineIndex) position(offset int) Position {
	lo, hi := 0, len(li.starts)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if li.starts[mid] <= offset {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	prefix := li.text[li.starts[lo]:offset]
	return Position{Line: lo, Character: len(utf16.Encode([]rune(prefix)))}
}

// hasLine reports whether a 1-based line exists in the document, so a comment
// pointing past the end can be flagged rather than silently shown at the top.
func (li *lineIndex) hasLine(line1 int) bool {
	return line1 >= 1 && line1 <= len(li.starts)
}

// lineRange covers a whole 1-based line, used when the anchor text is gone.
func (li *lineIndex) lineRange(line1 int) Range {
	idx := line1 - 1
	if idx < 0 || idx >= len(li.starts) {
		idx = 0
	}
	start := li.starts[idx]
	end := len(li.text)
	if idx+1 < len(li.starts) {
		end = li.starts[idx+1] - 1
	}
	return Range{Start: li.position(start), End: li.position(end)}
}

// locate resolves a comment's anchor against the current document text. It
// prefers the occurrence nearest the recorded line, which is what keeps
// anchors stable when a phrase repeats in the document.
// locateOffsets is locate in byte offsets, which is what a code action needs to
// carry across a resolve round trip.
func locateOffsets(li *lineIndex, c mrsf.Comment) (start, end int, ok bool) {
	if c.SelectedText == "" {
		return 0, 0, false
	}
	best := -1
	bestDist := 1 << 30
	for off := 0; ; {
		i := strings.Index(li.text[off:], c.SelectedText)
		if i < 0 {
			break
		}
		abs := off + i
		dist := li.position(abs).Line + 1 - c.Line
		if dist < 0 {
			dist = -dist
		}
		if dist < bestDist {
			bestDist, best = dist, abs
		}
		off = abs + 1
	}
	if best < 0 {
		return 0, 0, false
	}
	return best, best + len(c.SelectedText), true
}

func locate(li *lineIndex, c mrsf.Comment) (Range, bool) {
	if c.SelectedText == "" {
		return li.lineRange(c.Line), false
	}
	var best = -1
	bestDist := 1 << 30
	for off := 0; ; {
		i := strings.Index(li.text[off:], c.SelectedText)
		if i < 0 {
			break
		}
		abs := off + i
		dist := li.position(abs).Line + 1 - c.Line
		if dist < 0 {
			dist = -dist
		}
		if dist < bestDist {
			bestDist, best = dist, abs
		}
		off = abs + 1
	}
	if best < 0 {
		return li.lineRange(c.Line), false
	}
	return Range{
		Start: li.position(best),
		End:   li.position(best + len(c.SelectedText)),
	}, true
}
