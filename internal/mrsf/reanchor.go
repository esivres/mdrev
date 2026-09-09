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

	offset, length, score := findAnchor(text, c.SelectedText, c.Line)
	if offset < 0 {
		c.setExtra(ReanchorStatusKey, StatusOrphaned)
		delete(c.Extra, ReanchorScoreKey)
		return
	}

	line, column := position(text, offset)
	endLine, endColumn := position(text, offset+length)
	c.Line, c.EndLine = line, endLine
	c.StartColumn, c.EndColumn = &column, &endColumn
	c.setExtra(ReanchorStatusKey, StatusAnchored)
	c.setExtra(ReanchorScoreKey, score)
}

// minSimilarity is how much of the quoted text must still be recognisable.
// Below this the match says more about the length of the strings than about
// the text, and pointing a comment at the wrong sentence is worse than
// admitting the anchor is gone.
const minSimilarity = 0.6

// findAnchor locates the quoted text, exactly if it is still there and by
// resemblance if it has been edited. Editing the very sentence a comment is
// about is the ordinary case in review — the remark is usually why it changed —
// and losing the anchor exactly then is when it hurts most.
func findAnchor(text, quote string, line int) (offset, length int, score float64) {
	if at := nearestOccurrence(text, quote, line); at >= 0 {
		return at, len(quote), 1
	}

	lines := strings.Split(text, "\n")
	best, bestLen, bestScore, bestLine := -1, 0, 0.0, 0
	lineStart := 0
	wanted := wordSet(quote)

	for i, candidate := range lines {
		// Comparing every line character by character costs seconds on a long
		// document, and this runs on every write. A line that shares no word
		// with the quote cannot resemble it, and that check is cheap.
		if sharesWords(candidate, wanted) {
			if at, length, s := bestFragment(candidate, quote); s > bestScore ||
				(s == bestScore && best >= 0 && closer(i, bestLine, line-1)) {
				best, bestLen, bestScore, bestLine = lineStart+at, length, s, i
			}
		}
		lineStart += len(candidate) + 1
	}
	if best < 0 || bestScore < minSimilarity {
		return -1, 0, 0
	}
	return best, bestLen, bestScore
}

// bestFragment finds the run of words in a line that most resembles the quote.
// Comparing against the whole line would not do: a quote is usually a fragment
// of one, and the surrounding words alone would push it below the threshold.
func bestFragment(line, quote string) (offset, length int, score float64) {
	words := wordSpans(line)
	if len(words) == 0 {
		return 0, 0, 0
	}
	want := len(strings.Fields(quote))
	best, bestLen, bestScore := 0, 0, 0.0

	for i := range words {
		// A rewrite adds or drops a word or two, so the window is allowed to
		// breathe around the original length.
		for n := max(1, want-2); n <= want+2 && i+n <= len(words); n++ {
			start, end := words[i].start, words[i+n-1].end
			if s := similarity(quote, line[start:end]); s > bestScore {
				best, bestLen, bestScore = start, end-start, s
			}
		}
	}
	return best, bestLen, bestScore
}

// minShared is how much of the quote's vocabulary a line must carry to be
// worth comparing properly. A rewrite keeps most of its words; a line that
// keeps almost none is a different sentence.
const minShared = 0.4

func wordSet(text string) map[string]bool {
	set := map[string]bool{}
	for _, word := range strings.Fields(strings.ToLower(text)) {
		set[word] = true
	}
	return set
}

func sharesWords(line string, wanted map[string]bool) bool {
	if len(wanted) == 0 {
		return false
	}
	found := 0
	for _, word := range strings.Fields(strings.ToLower(line)) {
		if wanted[word] {
			found++
		}
	}
	return float64(found)/float64(len(wanted)) >= minShared
}

type span struct{ start, end int }

func wordSpans(line string) []span {
	var spans []span
	start := -1
	for i, r := range line {
		if r == ' ' || r == '\t' {
			if start >= 0 {
				spans = append(spans, span{start, i})
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		spans = append(spans, span{start, len(line)})
	}
	return spans
}

func closer(candidate, current, to int) bool {
	return abs(candidate-to) < abs(current-to)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// similarity is the share of the longer string that the two have in common,
// by edit distance.
func similarity(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	longest := max(len(ra), len(rb))
	if longest == 0 {
		return 0
	}
	return 1 - float64(distance(ra, rb))/float64(longest)
}

// distance is Levenshtein over runes; the strings here are a sentence at most.
func distance(a, b []rune) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(min(current[j-1]+1, previous[j]+1), previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
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
