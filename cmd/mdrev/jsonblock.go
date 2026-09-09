package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Zed's keymap and tasks files are JSON with comments, hand-edited and often
// long. Parsing and re-emitting them would drop the user's comments and
// formatting, so our entries go in as a marked block of text and everything
// else is left byte for byte.
const (
	blockBegin = "// mdrev:begin — managed by `mdrev setup`, edit the keys and this stays"
	blockEnd   = "// mdrev:end"
)

// mergeBlock inserts entries into a JSON array file, replacing our previous
// block and any entry an older version left outside it. The file is created
// when absent.
func mergeBlock(path, entries string, ours ...string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	block := blockBegin + "\n" + entries + "\n" + blockEnd
	text := strings.TrimSpace(string(existing))

	if len(ours) > 0 && text != "" {
		if cleaned, removed := dropStaleEntries(text, ours); removed > 0 {
			text = strings.TrimSpace(cleaned)
			fmt.Printf("removed %d entry(s) written by an older mdrev in %s\n", removed, path)
		}
	}

	switch {
	case text == "":
		text = "[\n" + indent(block) + "\n]\n"
	case strings.Contains(text, blockBegin):
		text = replaceBlock(text, block) + "\n"
	default:
		text, err = appendToArray(text, block)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return err
	}
	fmt.Println("updated", path)
	return nil
}

func replaceBlock(text, block string) string {
	start := strings.Index(text, blockBegin)
	end := strings.Index(text[start:], blockEnd)
	if end < 0 {
		return text
	}
	end += start + len(blockEnd)
	// The first line sits where the old block began, so only the rest is
	// indented.
	return text[:start] + strings.TrimPrefix(indent(block), indentUnit) + text[end:]
}

// appendToArray puts the block just before the array's closing bracket, so the
// rest of the file — comments, order, spacing — is untouched. The opening
// bracket is found rather than assumed to be first: these files usually start
// with a comment.
func appendToArray(text, block string) (string, error) {
	open := strings.Index(text, "[")
	close := strings.LastIndex(text, "]")
	if open < 0 || close < open {
		return "", fmt.Errorf("expected a JSON array")
	}

	head := strings.TrimRight(text[:close], " \t\n")
	separator := ",\n"
	if strings.HasSuffix(head, "[") || strings.HasSuffix(head, ",") {
		separator = "\n"
	}
	return head + separator + indent(block) + "\n" + text[close:] + "\n", nil
}

const indentUnit = "  "

func indent(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indentUnit + line
		}
	}
	return strings.Join(lines, "\n")
}

// conflicts reports bindings already present in the file for the given keys, so
// setup can say what it is about to shadow instead of silently winning.
func conflicts(path string, keys []string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := string(data)
	if i := strings.Index(text, blockBegin); i >= 0 {
		if j := strings.Index(text[i:], blockEnd); j >= 0 {
			text = text[:i] + text[i+j:] // ignore our own block
		}
	}

	var found []string
	for _, key := range keys {
		if key != "" && strings.Contains(text, `"`+key+`"`) {
			found = append(found, key)
		}
	}
	return found
}

// boundTo reports what a key is already bound to in the user's keymap, ignoring
// our own block. It reads the text rather than parsing: the file is JSON with
// comments, and this only needs to be good enough to warn.
func boundTo(path, key string) string {
	data, err := os.ReadFile(path)
	if err != nil || key == "" {
		return ""
	}
	text := string(data)
	if i := strings.Index(text, blockBegin); i >= 0 {
		if j := strings.Index(text[i:], blockEnd); j >= 0 {
			text = text[:i] + text[i+j:]
		}
	}

	at := strings.Index(text, `"`+key+`":`)
	if at < 0 {
		return ""
	}
	rest := strings.TrimSpace(text[at+len(key)+3:])
	if end := strings.IndexAny(rest, ",\n}"); end > 0 {
		rest = rest[:end]
	}
	// A trailing line comment is common in these files and is not part of the
	// binding.
	if comment := strings.Index(rest, "//"); comment >= 0 {
		rest = rest[:comment]
	}
	if value := strings.Trim(strings.TrimSpace(rest), `"[ `); value != "" {
		return value
	}
	return "something"
}

// dropStaleEntries removes our own entries left outside the managed block by an
// earlier version, which wrote these files whole. Merging beside them leaves
// the editor with two of every task, and warning about it is no help: the file
// is already wrong by the time anyone reads the warning.
func dropStaleEntries(text string, names []string) (string, int) {
	body, head, tail := arrayBody(text)
	if body == "" {
		return text, 0
	}

	var kept []string
	removed := 0
	for _, entry := range topLevelObjects(body) {
		if entry.managed || !containsAny(entry.text, names) {
			kept = append(kept, entry.text)
			continue
		}
		removed++
	}
	if removed == 0 {
		return text, 0
	}
	return head + strings.Join(kept, ",\n") + tail, removed
}

type entry struct {
	text    string
	managed bool
}

// topLevelObjects splits an array body into its elements, keeping whatever sits
// between them — our block markers, and the user's comments — attached to the
// element that follows.
func topLevelObjects(body string) []entry {
	var out []entry
	depth, start, inString, escaped, inComment := 0, 0, false, false, false
	managed := false

	for i, r := range body {
		switch {
		case escaped:
			escaped = false
		case inString && r == '\\':
			escaped = true
		case r == '"':
			inString = !inString
		case inString:
		case inComment:
			if r == '\n' {
				inComment = false
			}
		case r == '/' && i+1 < len(body) && body[i+1] == '/':
			inComment = true
		case r == '{':
			if depth == 0 {
				start = i
				managed = strings.Contains(body[:i], blockBegin) &&
					!strings.Contains(body[:i], blockEnd)
			}
			depth++
		case r == '}':
			depth--
			if depth == 0 {
				out = append(out, entry{text: strings.TrimSpace(body[start : i+1]), managed: managed})
			}
		}
	}
	return out
}

// arrayBody returns what is inside the outermost brackets, and the text around
// them, so comments before the array survive.
func arrayBody(text string) (body, head, tail string) {
	open := strings.Index(text, "[")
	close := strings.LastIndex(text, "]")
	if open < 0 || close < open {
		return "", "", ""
	}
	return text[open+1 : close], text[:open+1] + "\n  ", "\n" + text[close:]
}

func containsAny(text string, names []string) bool {
	for _, name := range names {
		if strings.Contains(text, `"`+name+`"`) {
			return true
		}
	}
	return false
}

// staleEntries reports which of our entries sit outside the managed block.
func staleEntries(path string, names []string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	text := string(data)
	if i := strings.Index(text, blockBegin); i >= 0 {
		if j := strings.Index(text[i:], blockEnd); j >= 0 {
			text = text[:i] + text[i+j:]
		}
	}

	var found []string
	for _, name := range names {
		if strings.Contains(text, `"`+name+`"`) {
			found = append(found, name)
		}
	}
	return found
}
