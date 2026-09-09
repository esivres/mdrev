package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/esivres/mdrev/internal/lsp"
	"github.com/esivres/mdrev/internal/mrsf"
	"github.com/esivres/mdrev/internal/tui"
)

const usage = `mdrev — review markdown: comments, questions and suggested edits.
Comments live in a sidecar next to the document; the document is never modified.

  mdrev setup [flags]       configure the editor once for this machine
  mdrev init [flags]        prepare a project: instructions for coding agents
  mdrev comment [flags]     add a comment; text from stdin or --editor
  mdrev reply [flags]       reply in a thread
  mdrev list <file.md>      open comments; --json for an agent
  mdrev threads <file.md>   browse threads, reply and resolve, in a terminal UI
  mdrev lsp                 language server, started by the editor

  mdrev <command> -h        flags of a command

An anchor is --quote "fragment" plus --line N: the comment follows the text as
the document is edited, and is flagged as orphaned once the fragment is gone.
`

var version = "dev"

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "--version", "-v", "version":
		fmt.Println("mdrev", version)
	case "-h", "--help", "help":
		fmt.Print(usage)
	case "lsp":
		err = lsp.NewServer(os.Stdout).Run(os.Stdin)
	case "setup":
		err = setUpEditor(os.Args[2:])
	case "init":
		err = initProject(os.Args[2:])
	case "reply":
		err = replyToComment(os.Args[2:])
	case "list":
		if len(os.Args) < 3 {
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
		err = printComments(os.Args[2:])
	case "threads":
		if len(os.Args) < 3 {
			fmt.Fprint(os.Stderr, usage)
			os.Exit(2)
		}
		err = tui.Run(os.Args[2])
	case "comment":
		err = addComment(os.Args[2:])
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "mdrev:", err)
		os.Exit(1)
	}
}

func printComments(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "machine-readable output")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	sc, err := mrsf.Load(args[0])
	if err != nil {
		return err
	}

	open := []mrsf.Comment{}
	replies := map[string][]mrsf.Comment{}
	if sc != nil {
		for _, c := range sc.Comments {
			switch {
			case c.ReplyTo != "":
				replies[c.ReplyTo] = append(replies[c.ReplyTo], c)
			case !c.Resolved:
				open = append(open, c)
			}
		}
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(open)
	}
	for _, c := range open {
		fmt.Printf("%s  %s:%d", shortID(c.ID), filepath.Base(args[0]), c.Line)
		if c.Type != "" {
			fmt.Printf("  [%s]", c.Type)
		}
		fmt.Println()
		if c.SelectedText != "" {
			fmt.Printf("    %q\n", c.SelectedText)
		}
		printBody(c, "    ", "      ")
		if s, ok := c.SuggestedText(); ok {
			fmt.Printf("    -> %s\n", s)
		}
		for _, r := range replies[c.ID] {
			printBody(r, "    | ", "        ")
		}
		fmt.Println()
	}
	if len(open) == 0 {
		fmt.Println("No open comments.")
	}
	return nil
}

func printBody(c mrsf.Comment, authorPrefix, textPrefix string) {
	fmt.Printf("%s%s:\n", authorPrefix, c.Author)
	for _, line := range strings.Split(c.Text, "\n") {
		fmt.Printf("%s%s\n", textPrefix, line)
	}
}

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func addComment(args []string) error {
	fs := flag.NewFlagSet("comment", flag.ExitOnError)
	file := fs.String("file", "", "path to the document")
	line := fs.Int("line", 0, "1-based line the comment anchors to")
	quote := fs.String("quote", "", "selected text to anchor to")
	text := fs.String("text", "", "comment text; read from stdin when empty")
	author := fs.String("author", "", "comment author")
	kind := fs.String("type", "", "issue | suggestion | question")
	severity := fs.String("severity", "", "low | medium | high")
	suggest := fs.String("suggest", "", "replacement text offered as a fix")
	useEditor := fs.Bool("editor", false, "compose the comment in $EDITOR instead of stdin")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" {
		return fmt.Errorf("--file is required")
	}

	// Zed hands multi-line selections through verbatim; the first line is
	// enough to anchor on and keeps the sidecar readable.
	*quote = strings.TrimSpace(firstLine(*quote))

	if *text == "" {
		body, err := readText(*quote, *line, *useEditor)
		if err != nil {
			return err
		}
		*text = body
	}
	if *text == "" {
		return fmt.Errorf("comment text is empty")
	}

	sidecar, err := mrsf.LoadOrCreate(*file)
	if err != nil {
		return err
	}
	c := mrsf.Comment{
		Author:       cmp(*author, gitUserName()),
		Text:         *text,
		Line:         *line,
		SelectedText: *quote,
		Type:         *kind,
		Severity:     *severity,
	}
	if *suggest != "" {
		c.Extra = map[string]any{mrsf.SuggestedTextKey: *suggest}
	}
	added, err := sidecar.Add(c)
	if err != nil {
		return err
	}
	if err := sidecar.Save(); err != nil {
		return err
	}
	fmt.Printf("Added comment %s to %s\n", shortID(added.ID), mrsf.Path(*file))
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func cmp(preferred, fallback string) string {
	if preferred != "" {
		return preferred
	}
	return fallback
}

func gitUserName() string {
	out, err := exec.Command("git", "config", "user.name").Output()
	if err != nil {
		return os.Getenv("USER")
	}
	if name := strings.TrimSpace(string(out)); name != "" {
		return name
	}
	return os.Getenv("USER")
}
