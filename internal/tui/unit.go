package tui

import (
	"regexp"
	"strings"
)

// Markdown has units of its own — a list item, a table row, a fenced block, a
// heading — and they are what a comment is about. Taking the run between blank
// lines instead means a list of forty items is one "paragraph", and every
// thread inside it shows the same wall of text.
var (
	listMarker  = regexp.MustCompile(`^\s*([-*+]|\d+[.)])\s`)
	tableRow    = regexp.MustCompile(`^\s*\|`)
	headingLine = regexp.MustCompile(`^\s*#{1,6}\s`)
	fenceLine   = regexp.MustCompile("^\\s*(```|~~~)")
)

// unitAt returns the lines of the markdown element containing idx.
func unitAt(lines []string, idx int) (first, last int) {
	if first, last, ok := fenceAround(lines, idx); ok {
		return first, last
	}

	line := lines[idx]
	switch {
	case headingLine.MatchString(line), tableRow.MatchString(line):
		// A heading stands alone, and a table row is a record in itself.
		return idx, idx

	case listMarker.MatchString(line) || continuesListItem(lines, idx):
		return listItemAround(lines, idx)

	default:
		return paragraphAround(lines, idx)
	}
}

// A fenced block is quoted whole: half of one says nothing.
func fenceAround(lines []string, idx int) (first, last int, ok bool) {
	open := -1
	for i := 0; i <= idx; i++ {
		if fenceLine.MatchString(lines[i]) {
			if open < 0 {
				open = i
			} else {
				open = -1
			}
		}
	}
	if open < 0 {
		return 0, 0, false
	}
	for i := idx + 1; i < len(lines); i++ {
		if fenceLine.MatchString(lines[i]) {
			return open, i, true
		}
	}
	return open, len(lines) - 1, true
}

// A list item runs from its marker to the last line indented under it.
func listItemAround(lines []string, idx int) (first, last int) {
	first = idx
	for first > 0 && !listMarker.MatchString(lines[first]) {
		first--
	}
	last = first
	for last+1 < len(lines) {
		next := lines[last+1]
		if strings.TrimSpace(next) == "" || listMarker.MatchString(next) || headingLine.MatchString(next) {
			break
		}
		last++
	}
	return first, last
}

// continuesListItem reports whether a line is the indented continuation of an
// item above it rather than prose of its own.
func continuesListItem(lines []string, idx int) bool {
	if strings.TrimSpace(lines[idx]) == "" {
		return false
	}
	for i := idx - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) == "" {
			return false
		}
		if listMarker.MatchString(lines[i]) {
			return true
		}
	}
	return false
}

func paragraphAround(lines []string, idx int) (first, last int) {
	first, last = idx, idx
	for first > 0 && strings.TrimSpace(lines[first-1]) != "" &&
		!listMarker.MatchString(lines[first-1]) && !headingLine.MatchString(lines[first-1]) {
		first--
	}
	for last+1 < len(lines) && strings.TrimSpace(lines[last+1]) != "" &&
		!listMarker.MatchString(lines[last+1]) && !headingLine.MatchString(lines[last+1]) {
		last++
	}
	return first, last
}
