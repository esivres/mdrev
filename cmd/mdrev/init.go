package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/esivres/mdrev/internal/agentdocs"
)

// hostExtension is the extension whose language server slot mdrev occupies.
// Zed only lets an extension declare a server, and mdrev has none of its own
// yet, so it overrides the binary of one that does. Markdownlint is chosen
// over Marksman because Marksman is worth keeping alive alongside us: Zed runs
// several servers per language, so its link navigation survives.
const hostExtension = "markdownlint"

const hostNote = `Zed cannot register a language server from settings: only an extension can
declare one. mdrev works around this by taking over the Markdownlint
extension, which declares itself a server for Markdown, and overriding its
binary. Install Markdownlint from the Zed extensions panel.

Other Markdown servers keep running alongside mdrev, so Marksman stays useful.`

const commentTask = "Comment on selection"
const questionTask = "Question about selection"

// keyPresets are offered when init runs interactively. Anything else can be
// typed in, or passed with --keys.
var keyPresets = []string{"alt-c", "ctrl-alt-c", "ctrl-shift-m"}

func initProject(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	keys := fs.String("keys", "", "shortcut for the comment task, e.g. alt-c; a second one may follow after a comma")
	writeKeymap := fs.Bool("write-keymap", false, "write the shortcuts into Zed's global keymap")
	noKeymap := fs.Bool("no-keymap", false, "skip shortcuts entirely")
	agentDocs := fs.String("agent-docs", "", "instructions for coding agents: agents | skill | both | none")
	if err := fs.Parse(args); err != nil {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	if err := writeIfAbsent(filepath.Join(".zed", "settings.json"), zedSettings(exe, ownExtensionInstalled())); err != nil {
		return err
	}
	if err := writeIfAbsent(filepath.Join(".zed", "tasks.json"), zedTasks(exe)); err != nil {
		return err
	}

	if !ownExtensionInstalled() && !hostExtensionInstalled() {
		fmt.Printf("\n! Neither the mdrev extension nor %s is installed.\n", hostExtension)
		fmt.Println(hostNote)
	}

	if err := setUpAgentDocs(*agentDocs); err != nil {
		return err
	}

	if *noKeymap {
		return nil
	}
	comment, question, err := chooseKeys(*keys)
	if err != nil {
		return err
	}
	if comment == "" {
		return nil
	}
	return applyKeymap(comment, question, *writeKeymap)
}

// setUpAgentDocs installs the instructions that tell a coding agent how to use
// mdrev, so the human does not have to explain it in every session.
func setUpAgentDocs(choice string) error {
	if choice == "" {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return nil
		}
		fmt.Println("\nInstructions for coding agents:")
		fmt.Println("  1) AGENTS.md")
		fmt.Println("  2) Claude Code skill (.claude/skills/mdrev)")
		fmt.Println("  3) both")
		fmt.Println("  4) none  (default)")
		fmt.Print("> ")
		in := bufio.NewScanner(os.Stdin)
		if !in.Scan() {
			return nil
		}
		switch strings.TrimSpace(in.Text()) {
		case "1":
			choice = "agents"
		case "2":
			choice = "skill"
		case "3":
			choice = "both"
		default:
			return nil
		}
	}

	switch choice {
	case "", "none":
		return nil
	case "agents":
		return agentdocs.AppendToAgentsFile("AGENTS.md")
	case "skill":
		return agentdocs.WriteSkill(filepath.Join(".claude", "skills", "mdrev"))
	case "both":
		if err := agentdocs.AppendToAgentsFile("AGENTS.md"); err != nil {
			return err
		}
		return agentdocs.WriteSkill(filepath.Join(".claude", "skills", "mdrev"))
	default:
		return fmt.Errorf("unknown --agent-docs value %q", choice)
	}
}

// chooseKeys resolves the shortcuts from --keys, or asks when stdin is a
// terminal. Returning an empty comment key means "no shortcuts".
func chooseKeys(flagValue string) (comment, question string, err error) {
	if flagValue != "" {
		return splitKeys(flagValue)
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return splitKeys(keyPresets[0])
	}

	fmt.Println("\nShortcut for commenting on a selection:")
	for i, k := range keyPresets {
		suffix := ""
		if i == 0 {
			suffix = "  (default)"
		}
		fmt.Printf("  %d) %s%s\n", i+1, k, suffix)
	}
	fmt.Printf("  %d) enter your own\n", len(keyPresets)+1)
	fmt.Printf("  %d) no shortcuts\n", len(keyPresets)+2)
	fmt.Print("> ")

	in := bufio.NewScanner(os.Stdin)
	if !in.Scan() {
		return splitKeys(keyPresets[0])
	}
	switch choice := strings.TrimSpace(in.Text()); choice {
	case "":
		return splitKeys(keyPresets[0])
	case "1", "2", "3":
		return splitKeys(keyPresets[int(choice[0]-'1')])
	case "4":
		fmt.Print("Shortcut (Zed syntax, e.g. ctrl-alt-k): ")
		if !in.Scan() {
			return "", "", fmt.Errorf("no shortcut given")
		}
		return splitKeys(strings.TrimSpace(in.Text()))
	case "5":
		return "", "", nil
	default:
		return "", "", fmt.Errorf("unknown choice %q", choice)
	}
}

// splitKeys accepts "alt-c" or "alt-c,alt-shift-c". With one key given, the
// question shortcut is its shift variant.
func splitKeys(value string) (comment, question string, err error) {
	parts := strings.Split(value, ",")
	comment = strings.TrimSpace(parts[0])
	if comment == "" {
		return "", "", fmt.Errorf("empty shortcut")
	}
	if len(parts) > 1 {
		return comment, strings.TrimSpace(parts[1]), nil
	}
	return comment, shiftVariant(comment), nil
}

func shiftVariant(key string) string {
	if strings.Contains(key, "shift-") {
		return ""
	}
	i := strings.LastIndex(key, "-")
	return key[:i+1] + "shift-" + key[i+1:]
}

func applyKeymap(comment, question string, write bool) error {
	path := filepath.Join(os.Getenv("HOME"), ".config", "zed", "keymap.json")
	content := zedKeymap(comment, question)
	if write {
		return writeIfAbsent(path, content)
	}
	fmt.Printf("\nZed keymaps are global. Add to %s:\n%s\n", path, content)
	return nil
}

// writeIfAbsent never overwrites: these files usually hold the user's own
// settings, and merging JSON blind is worse than printing what to paste.
func writeIfAbsent(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("\n%s exists, leaving it alone. Add this:\n%s\n", path, content)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	fmt.Println("created", path)
	return nil
}

func hostExtensionInstalled() bool {
	_, err := os.Stat(filepath.Join(os.Getenv("HOME"),
		".local/share/zed/extensions/installed", hostExtension, "extension.toml"))
	return err == nil
}

// zedSettings enables mdrev for Markdown. With our own extension installed the
// server is simply named; without it we borrow another extension's slot, since
// Zed will not register a server that no extension declares. Either way "..."
// keeps the other Markdown servers running next to us.
func zedSettings(exe string, ownExtension bool) string {
	if ownExtension {
		return mustJSON(map[string]any{
			"languages": map[string]any{
				"Markdown": map[string]any{"language_servers": []string{"mdrev", "..."}},
			},
		})
	}
	return mustJSON(map[string]any{
		"languages": map[string]any{
			"Markdown": map[string]any{"language_servers": []string{hostExtension, "..."}},
		},
		"lsp": map[string]any{
			hostExtension: map[string]any{
				"binary": map[string]any{
					"path":                  exe,
					"arguments":             []string{"lsp"},
					"ignore_system_version": true,
				},
			},
		},
	})
}

func ownExtensionInstalled() bool {
	_, err := os.Stat(filepath.Join(os.Getenv("HOME"),
		".local/share/zed/extensions/installed/mdrev/extension.toml"))
	return err == nil
}

func zedTasks(exe string) string {
	task := func(label string, extra ...string) map[string]any {
		args := append([]string{"comment",
			"--file", "$ZED_FILE",
			"--line", "$ZED_ROW",
			"--quote", "$ZED_SELECTED_TEXT",
		}, extra...)
		return map[string]any{
			"label":                 label,
			"command":               exe,
			"args":                  args,
			"use_new_terminal":      false,
			"allow_concurrent_runs": false,
			"reveal":                "always",
			"reveal_target":         "dock",
		}
	}
	return mustJSON([]any{
		task(commentTask),
		task(questionTask, "--type", "question"),
	})
}

func zedKeymap(comment, question string) string {
	bindings := map[string]any{
		comment: []any{"task::Spawn", map[string]any{"task_name": commentTask}},
	}
	if question != "" {
		bindings[question] = []any{"task::Spawn", map[string]any{"task_name": questionTask}}
	}
	return mustJSON([]any{map[string]any{
		"context":  "Editor",
		"bindings": bindings,
	}})
}

func mustJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(b) + "\n"
}
