// Package anchor quotes a fragment of a document to attach a comment to.
// The quote has to be long enough to be unique and short enough to survive
// later edits, and it must read well when the comment is listed.
package anchor

import (
	"strings"
	"unicode"
)

// Words is how much text is quoted. Six words is about a clause.
const Words = 6

// Before quotes the end of a text, which is where a remark written after a
// fragment belongs.
func Before(s string) string {
	fields := strings.Fields(s)
	if len(fields) > Words {
		fields = fields[len(fields)-Words:]
	}
	return trim(strings.Join(fields, " "))
}

// After quotes the beginning of the first non-empty line, used when there is
// nothing in front of the comment to anchor on.
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
