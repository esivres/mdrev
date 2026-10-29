package mrsf

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strings"
	"unicode/utf8"
)

// Anchor status values, matching what the reference tool writes.
const (
	ReanchorStatusKey = "x_reanchor_status"
	ReanchorScoreKey  = "x_reanchor_score"
	// ReanchorDocKey holds a digest of the document text the comments were
	// last anchored against. A save that can compare against it skips the
	// whole re-anchoring pass when the document has not changed — a pass
	// that cannot move a single position but on a large review costs enough
	// to time out another writer waiting on the lock.
	ReanchorDocKey = "x_reanchor_doc"
	StatusAnchored = "anchored"
	StatusOrphaned = "orphaned"
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
	ix := newDocIndex(text)
	for i := range s.Comments {
		ix.reanchor(&s.Comments[i])
	}
}

// reanchorTo anchors the review against text and records the digest that lets
// the next save skip the work. Comments that carry no status yet — freshly
// added, or written by a tool that does no anchoring — are anchored even when
// the digest matches, so they arrive scored and positioned like the rest.
func reanchorTo(s *Sidecar, text string) {
	sum := sha256.Sum256([]byte(text))
	digest := hex.EncodeToString(sum[:])

	if recorded, _ := s.Extra[ReanchorDocKey].(string); recorded == digest {
		ix := newDocIndex(text)
		for i := range s.Comments {
			c := &s.Comments[i]
			if c.SelectedText == "" {
				continue
			}
			// A comment another tool wrote may carry a status but no hash;
			// the hash is spec-visible and must not depend on the document
			// having changed in between.
			if c.SelectedTextHash == "" {
				sum := sha256.Sum256([]byte(c.SelectedText))
				c.SelectedTextHash = hex.EncodeToString(sum[:])
			}
			if c.Extra[ReanchorStatusKey] == nil {
				ix.reanchor(c)
			}
		}
		return
	}

	s.Reanchor(text)
	if s.Extra == nil {
		s.Extra = map[string]any{}
	}
	s.Extra[ReanchorDocKey] = digest
}

func reanchor(text string, c *Comment) {
	newDocIndex(text).reanchor(c)
}

func (ix *docIndex) reanchor(c *Comment) {
	if c.SelectedText == "" {
		return // a line-scoped comment has nothing to follow
	}
	if c.SelectedTextHash == "" {
		sum := sha256.Sum256([]byte(c.SelectedText))
		c.SelectedTextHash = hex.EncodeToString(sum[:])
	}

	offset, length, score := ix.findAnchor(c.SelectedText, c.Line)
	if offset < 0 {
		c.setExtra(ReanchorStatusKey, StatusOrphaned)
		delete(c.Extra, ReanchorScoreKey)
		return
	}

	line, column := position(ix.text, offset)
	endLine, endColumn := position(ix.text, offset+length)
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

// docIndex is the document prepared for one anchoring pass. Splitting it,
// tokenising it and searching it once per pass instead of once per comment is
// the difference between milliseconds and minutes on a long review.
type docIndex struct {
	text      string
	lines     []string
	lineStart []int             // byte offset of each line's first character
	lineWords []map[string]bool // each line's distinct lowercase words
	postings  map[string][]int  // lowercase word -> the lines carrying it
}

func newDocIndex(text string) *docIndex {
	ix := &docIndex{text: text, postings: map[string][]int{}}
	off := 0
	for _, line := range strings.Split(text, "\n") {
		ix.lines = append(ix.lines, line)
		ix.lineStart = append(ix.lineStart, off)
		off += len(line) + 1

		words := map[string]bool{}
		for _, word := range strings.Fields(strings.ToLower(line)) {
			if !words[word] {
				words[word] = true
				ix.postings[word] = append(ix.postings[word], len(ix.lines)-1)
			}
		}
		ix.lineWords = append(ix.lineWords, words)
	}
	return ix
}

// offsetOf returns the byte offset of a recorded 1-based line, clamped to the
// document: a decayed position may point past its end.
func (ix *docIndex) offsetOf(line int) int {
	if line < 1 {
		return 0
	}
	if line > len(ix.lines) {
		return len(ix.text)
	}
	return ix.lineStart[line-1]
}

// findAnchor locates the quoted text, exactly if it is still there and by
// resemblance if it has been edited. Editing the very sentence a comment is
// about is the ordinary case in review — the remark is usually why it changed —
// and losing the anchor exactly then is when it hurts most.
func (ix *docIndex) findAnchor(quote string, line int) (offset, length int, score float64) {
	if at := ix.nearestOccurrence(quote, line); at >= 0 {
		return at, len(quote), 1
	}

	best, bestLen, bestScore, bestLine := -1, 0, 0.0, 0
	wanted := wordSet(quote)

	for _, i := range ix.candidates(line, wanted) {
		// A line that shares no word with the quote cannot resemble it,
		// and that check is cheap.
		if ix.sharesWords(i, wanted) {
			if at, length, s := bestFragment(ix.lines[i], quote, wanted); s > bestScore ||
				(s == bestScore && best >= 0 && closer(i, bestLine, line-1)) {
				best, bestLen, bestScore, bestLine = ix.lineStart[i]+at, length, s, i
			}
		}
	}
	if best < 0 || bestScore < minSimilarity {
		return -1, 0, 0
	}
	return best, bestLen, bestScore
}

// neighborhood is how far around the recorded line an edited sentence is
// still looked for closely. Text rarely travels far between saves; beyond
// this, distinctive words carry the search.
const neighborhood = 12

// candidates are the lines the resemblance search runs over: a window around
// the recorded line, plus every line carrying one of the quote's distinctive
// words. A word that most lines carry — prose is mostly made of those — says
// nothing about where the text went, and comparing against every line is what
// a pass over a long review could not afford.
func (ix *docIndex) candidates(line int, wanted map[string]bool) []int {
	cutoff := max(8, len(ix.lines)/8)
	seen := map[int]bool{}
	var out []int

	center := line - 1
	for i := max(0, center-neighborhood); i <= center+neighborhood && i < len(ix.lines); i++ {
		seen[i] = true
		out = append(out, i)
	}
	for word := range wanted {
		if lines := ix.postings[word]; len(lines) > 0 && len(lines) <= cutoff {
			for _, i := range lines {
				if !seen[i] {
					seen[i] = true
					out = append(out, i)
				}
			}
		}
	}

	// Ascending order, so ties resolve toward the earlier line as before.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// minShared is how much of the quote's vocabulary a line must carry to be
// worth comparing properly. A rewrite keeps most of its words; a line that
// keeps almost none is a different sentence.
const minShared = 0.4

// sharedNeeded is the whole-number form of the minShared bar: the count of
// the quote's distinct words a line or window must carry.
func sharedNeeded(wanted int) int {
	return int(math.Ceil(minShared * float64(wanted)))
}

func (ix *docIndex) sharesWords(i int, wanted map[string]bool) bool {
	if len(wanted) == 0 {
		return false
	}
	found := 0
	for word := range wanted {
		if ix.lineWords[i][word] {
			found++
		}
	}
	return found >= sharedNeeded(len(wanted))
}

func wordSet(text string) map[string]bool {
	set := map[string]bool{}
	for _, word := range strings.Fields(strings.ToLower(text)) {
		set[word] = true
	}
	return set
}

// bestFragment finds the run of words in a line that most resembles the quote.
// Comparing against the whole line would not do: a quote is usually a fragment
// of one, and the surrounding words alone would push it below the threshold.
// A window that does not carry the quote's vocabulary is skipped before the
// edit distance runs: the distance is the expensive part, and a rewrite that
// keeps resemblance keeps words too.
func bestFragment(line, quote string, wanted map[string]bool) (offset, length int, score float64) {
	words := wordSpans(line)
	if len(words) == 0 {
		return 0, 0, 0
	}
	quoteRunes := []rune(quote)
	want := len(strings.Fields(quote))
	need := sharedNeeded(len(wanted))
	best, bestLen, bestScore := 0, 0, 0.0

	for i := range words {
		// A rewrite adds or drops a word or two, so the window is allowed to
		// breathe around the original length.
		for n := max(1, want-2); n <= want+2 && i+n <= len(words); n++ {
			start, end := words[i].start, words[i+n-1].end
			fragment := line[start:end]
			if carriesWords(line, words, i, n, wanted) < need {
				continue
			}
			if s, ok := similarityWithin(quoteRunes, fragment, bestScore); ok && s > bestScore {
				best, bestLen, bestScore = start, end-start, s
			}
		}
	}
	return best, bestLen, bestScore
}

// carriesWords counts the quote's distinct words one window of a line holds.
func carriesWords(line string, words []span, i, n int, wanted map[string]bool) int {
	found := 0
	var held []string
	for k := i; k < i+n; k++ {
		word := strings.ToLower(line[words[k].start:words[k].end])
		if !wanted[word] {
			continue
		}
		repeat := false
		for _, seen := range held {
			if seen == word {
				repeat = true
				break
			}
		}
		if !repeat {
			held = append(held, word)
			found++
		}
	}
	return found
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

// similarityWithin is the share of the longer string that the two have in
// common, by edit distance. A pair that cannot reach the floor — the match
// threshold, or the best score seen so far — is abandoned mid-measurement:
// most pairs are nothing alike, and the distance is the expensive part.
func similarityWithin(quote []rune, fragment string, floor float64) (float64, bool) {
	other := []rune(fragment)
	longest := max(len(quote), len(other))
	if longest == 0 {
		return 0, false
	}
	floor = max(floor, minSimilarity)
	maxEdits := int((1 - floor) * float64(longest))
	if abs(len(quote)-len(other)) > maxEdits {
		return 0, false
	}
	if d := distanceWithin(quote, other, maxEdits); d <= maxEdits {
		return 1 - float64(d)/float64(longest), true
	}
	return 0, false
}

// distanceWithin is Levenshtein over runes, abandoned as soon as the result
// must exceed maxEdits; the strings here are a sentence at most.
func distanceWithin(a, b []rune, maxEdits int) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		rowMin := current[0]
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(min(current[j-1]+1, previous[j]+1), previous[j-1]+cost)
			if current[j] < rowMin {
				rowMin = current[j]
			}
		}
		if rowMin > maxEdits {
			return maxEdits + 1
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

// nearestOccurrence picks the occurrence closest to the recorded line: a
// fragment can repeat, in prose and again inside a diagram, and the recorded
// line is the only hint of which one the comment meant. The nearest
// occurrence on either side of the recorded line is the nearest overall —
// every other one sits farther from it — so the search only has to find the
// first hit at or after the line and the last one before it.
func (ix *docIndex) nearestOccurrence(needle string, line int) int {
	anchor := ix.offsetOf(line)
	best, bestDist := -1, 0
	consider := func(at int) {
		found, _ := position(ix.text, at)
		dist := abs(found - line)
		if best < 0 || dist < bestDist {
			best, bestDist = at, dist
		}
	}

	at := strings.Index(ix.text[anchor:], needle)
	if at < 0 {
		// Nothing starts at or after the recorded line: walk what starts
		// before it. The window ends past the anchor so an occurrence
		// straddling it — possible when a foreign tool recorded a line
		// inside a multi-line quote — is contained. Several hits can share
		// a line; an exact tie keeps the earlier one, so every hit is
		// considered, in offset order, as the full scan did.
		end := min(anchor+len(needle)-1, len(ix.text))
		for off := 0; off < end; {
			i := strings.Index(ix.text[off:end], needle)
			if i < 0 {
				break
			}
			consider(off + i)
			off += i + 1
		}
		return best
	}

	// An occurrence at or after the line: only the lines no farther from it
	// than that hit can hold a better one. The window ends past the anchor
	// so an occurrence straddling it — possible when a foreign tool
	// recorded a line inside a multi-line quote — is contained. The
	// earlier one is considered first, so an exact tie still keeps the
	// earlier occurrence, as a scan in offset order did.
	found, _ := position(ix.text, anchor+at)
	afterDist := abs(found - line)
	if from := ix.offsetOf(line - afterDist); from < anchor {
		end := min(anchor+len(needle)-1, len(ix.text))
		for off := from; off < end; {
			i := strings.Index(ix.text[off:end], needle)
			if i < 0 {
				break
			}
			consider(off + i)
			off += i + 1
		}
	}
	consider(anchor + at)
	return best
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
