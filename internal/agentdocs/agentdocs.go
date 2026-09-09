// Package agentdocs carries the instructions that teach a coding agent how to
// use mdrev, so a human does not have to explain it in every session.
package agentdocs

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

//go:embed instructions.md
var instructions string

const heading = "## Reviewing markdown with mdrev"

// AppendToAgentsFile adds a section to AGENTS.md, creating the file when
// absent and leaving it alone when the section is already there.
func AppendToAgentsFile(path string) error {
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if strings.Contains(string(existing), heading) {
		fmt.Printf("%s already documents mdrev, leaving it alone\n", path)
		return nil
	}

	var b strings.Builder
	if len(existing) > 0 {
		b.Write(existing)
		if !strings.HasSuffix(string(existing), "\n") {
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(heading + "\n\n" + instructions)

	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return err
	}
	if len(existing) > 0 {
		fmt.Println("appended mdrev section to", path)
	} else {
		fmt.Println("created", path)
	}
	return nil
}

// WriteSkill writes a Claude Code skill, which is the same instructions with
// the front matter that makes them loadable on demand.
func WriteSkill(dir string) error {
	path := filepath.Join(dir, "SKILL.md")
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("%s exists, leaving it alone\n", path)
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	front := "---\nname: mdrev\ndescription: " +
		"Read and answer review comments on markdown documents, and propose edits " +
		"the author can apply. Use when a document is under review, when asked what " +
		"the review says, or after handing a document over for reading.\n---\n\n"
	if err := os.WriteFile(path, []byte(front+instructions), 0o644); err != nil {
		return err
	}
	fmt.Println("created", path)
	return nil
}
