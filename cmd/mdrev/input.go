package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// readText collects the comment body. Multi-line is the normal case for review
// prose, so stdin is read to EOF rather than a single line.
func readText(quote string, line int, useEditor bool) (string, error) {
	if useEditor {
		return readFromEditor(quote, line)
	}
	fmt.Println(header(quote, line))
	fmt.Println("Введите текст. Завершить — Ctrl+D на пустой строке.")
	fmt.Print("> ")
	body, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(body)), nil
}

func header(quote string, line int) string {
	if quote != "" {
		return fmt.Sprintf("Комментарий к «%s»", quote)
	}
	return fmt.Sprintf("Комментарий к строке %d", line)
}

// readFromEditor mirrors how git collects a commit message: a scratch file
// with a commented-out prompt, everything after '#' stripped.
func readFromEditor(quote string, line int) (string, error) {
	editor := firstNonEmpty(os.Getenv("VISUAL"), os.Getenv("EDITOR"))
	if editor == "" {
		return "", fmt.Errorf("--editor requires $VISUAL or $EDITOR to be set")
	}

	f, err := os.CreateTemp("", "mdrev-*.md")
	if err != nil {
		return "", err
	}
	path := f.Name()
	defer os.Remove(path)
	fmt.Fprintf(f, "\n\n# %s\n# Строки, начинающиеся с #, будут отброшены.\n# Пустой текст отменяет комментарий.\n", header(quote, line))
	f.Close()

	// The editor command may carry flags, e.g. EDITOR="zed --wait".
	parts := strings.Fields(editor)
	cmd := exec.Command(parts[0], append(parts[1:], path)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w", filepath.Base(parts[0]), err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var kept []string
	for _, l := range strings.Split(string(raw), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(l), "#") {
			kept = append(kept, l)
		}
	}
	return strings.TrimSpace(strings.Join(kept, "\n")), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
