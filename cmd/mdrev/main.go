package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
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
  mdrev apply [flags]       apply a comment's suggested edit to the document
  mdrev resolve [flags]     close a thread without replying
  mdrev list <file.md>      open comments; --json for an agent
  mdrev threads <file.md>   browse threads, reply and resolve, in a terminal UI
  mdrev lsp                 language server, started by the editor

  mdrev <command> -h        flags of a command

An anchor is --quote "fragment" plus --line N: the comment follows the text as
the document is edited, and is flagged as orphaned once the fragment is gone.
`

// Stamped by the release build; go install leaves it unset, so fall back to
// the module version the toolchain records.
var version = ""

func versionString() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "dev"
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "--version", "-v", "version":
		fmt.Println("mdrev", versionString())
	case "-h", "--help", "help":
		fmt.Print(usage)
	case "lsp":
		if len(os.Args) > 2 {
			fmt.Fprintln(os.Stderr, "mdrev lsp speaks the language server protocol on stdio; editors start it")
			os.Exit(2)
		}
		lsp.Version = versionString()
		err = lsp.NewServer(os.Stdout).Run(os.Stdin)
	case "setup":
		err = setUpEditor(os.Args[2:])
	case "init":
		err = initProject(os.Args[2:])
	case "reply":
		err = replyToComment(os.Args[2:])
	case "apply":
		err = applySuggestion(os.Args[2:])
	case "resolve":
		err = resolveComment(os.Args[2:])
	case "list":
		err = printComments(os.Args[2:])
	case "threads":
		err = browseThreads(os.Args[2:])
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

func browseThreads(args []string) error {
	fs := flag.NewFlagSet("threads", flag.ExitOnError)
	line := fs.Int("line", 0, "line the reader is on; opens the thread about it")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) < 1 {
		return fmt.Errorf("usage: mdrev threads <file.md> [--line N]")
	}
	if err := requireDocument(rest[0]); err != nil {
		return err
	}
	return tui.Run(rest[0], *line)
}

func printComments(args []string) error {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "machine-readable output")
	all := fs.Bool("all", false, "include resolved threads and how they ended")
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) < 1 {
		return fmt.Errorf("usage: mdrev list <file.md> [--json]")
	}
	document := rest[0]
	if err := requireDocument(document); err != nil {
		return err
	}
	sc, err := mrsf.Load(document)
	if err != nil {
		return err
	}

	var threads []mrsf.Thread
	if sc != nil {
		threads = sc.Threads(*all)
	}

	if *asJSON {
		// An empty slice, not nil: an agent parsing this should get [] rather
		// than null when a review is clean.
		out := []jsonComment{}
		for _, t := range threads {
			out = append(out, toJSON(t.Parent, t.Replies))
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	}
	for _, t := range threads {
		c := t.Parent
		fmt.Printf("%s  %s:%d", shortID(c.ID), filepath.Base(document), c.Line)
		if c.Type != "" {
			fmt.Printf("  [%s]", c.Type)
		}
		if outcome := c.Outcome(); outcome != "" {
			fmt.Printf("  (%s)", outcome)
		}
		if c.Orphaned() {
			fmt.Print("  [anchor lost]")
		}
		fmt.Println()
		if c.SelectedText != "" {
			fmt.Printf("    %q\n", c.SelectedText)
		}
		printBody(c, "    ", "      ")
		if s, ok := c.SuggestedText(); ok {
			fmt.Printf("    -> %s\n", s)
		}
		for _, r := range t.Replies {
			printBody(r, "    | ", "        ")
		}
		fmt.Println()
	}
	if len(threads) == 0 {
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
	rest, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(rest) > 0 {
		return fmt.Errorf("unexpected argument %q; the document is given with --file", rest[0])
	}
	if *file == "" {
		return fmt.Errorf("--file is required")
	}
	if err := requireDocument(*file); err != nil {
		return err
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

	c := mrsf.Comment{
		Author:       firstNonEmpty(*author, mrsf.DefaultAuthor()),
		Text:         *text,
		Line:         *line,
		SelectedText: *quote,
		Type:         *kind,
		Severity:     *severity,
	}
	if *suggest != "" {
		c.Extra = map[string]any{mrsf.SuggestedTextKey: *suggest}
	}

	var added *mrsf.Comment
	if err := mrsf.Update(*file, func(sc *mrsf.Sidecar) error {
		var err error
		added, err = sc.Add(c)
		return err
	}); err != nil {
		return err
	}
	fmt.Printf("Added comment %s to %s\n", shortID(added.ID), mrsf.Path(*file))
	return nil
}

// requireDocument turns a typo into an error. Without it a misspelled path is
// indistinguishable from a document with nothing to review, and a comment can
// be recorded against a file that does not exist.
func requireDocument(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
