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
// block if there is one. The file is created when absent.
func mergeBlock(path, entries string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	block := blockBegin + "\n" + entries + "\n" + blockEnd
	text := strings.TrimSpace(string(existing))

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
	return text[:start] + strings.TrimSpace(indent(block))[len(indentUnit):] + text[end:]
}

// appendToArray puts the block just before the array's closing bracket, so the
// rest of the file — comments, order, spacing — is untouched.
func appendToArray(text, block string) (string, error) {
	close := strings.LastIndex(text, "]")
	if close < 0 || !strings.HasPrefix(text, "[") {
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
