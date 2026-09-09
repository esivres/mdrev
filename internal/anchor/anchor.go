// Package anchor quotes a fragment of a document to attach a comment to: long
// enough to be unique, short enough to survive later edits.
package anchor

import (
	"strings"
	"unicode"
)

// Words is how much text is quoted — about a clause.
const Words = 6

// Before quotes the end of a text, where a remark written after a fragment
// belongs.
func Before(s string) string {
	fields := strings.Fields(s)
	if len(fields) > Words {
		fields = fields[len(fields)-Words:]
	}
	return trim(strings.Join(fields, " "))
}

// After quotes the first non-empty line, for a comment with nothing in front
// of it.
func After(s string) string {
	for _, line := range strings.Split(s, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) > Words {
			fields = fields[:Words]
		}
		return trim(strings.Join(fields, " "))
	}
	return ""
}

func trim(s string) string {
	return strings.TrimFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune(",;:", r)
	})
}

// NearestLine returns the 0-based line of the occurrence closest to a line
// already known, or that line if the needle is absent. A fragment can repeat —
// in prose and again inside a diagram — so any occurrence is not good enough.
func NearestLine(lines []string, needle string, near int) int {
	if needle == "" {
		return near
	}
	best, bestDist := -1, 1<<30
	for i, l := range lines {
		if !strings.Contains(l, needle) {
			continue
		}
		d := i - near
		if d < 0 {
			d = -d
		}
		if d < bestDist {
			best, bestDist = i, d
		}
	}
	if best < 0 {
		return near
	}
	return best
}

// Found reports whether needle occurs at all.
func Found(lines []string, needle string) bool {
	for _, l := range lines {
		if strings.Contains(l, needle) {
			return true
		}
	}
	return false
}
