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

const extensionNote = `Install it from the Zed extensions panel; Zed then starts mdrev for Markdown
on its own, with no per-project settings.

Until then mdrev borrows the ` + hostExtension + ` extension's server slot, which
means that extension has to be installed instead. Either way other Markdown
servers keep running alongside mdrev, so Marksman stays useful.`

const commentTask = "Comment on selection"
const questionTask = "Question about selection"
const threadsTask = "Review threads"

// keyPresets are offered when init runs interactively. Anything else can be
// typed in, or passed with --keys.
var keyPresets = []string{"alt-c", "ctrl-alt-c", "ctrl-shift-m"}

// setUpEditor configures Zed once for the machine: tasks and shortcuts are
// global, so no project needs to repeat them.
func setUpEditor(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	keys := fs.String("keys", "", "shortcut for the comment task, e.g. alt-c; a second one may follow after a comma")
	writeKeymap := fs.Bool("write-keymap", false, "write the shortcuts into Zed's keymap instead of printing them")
	noKeymap := fs.Bool("no-keymap", false, "skip shortcuts entirely")
	if err := fs.Parse(args); err != nil {
		return err
	}

	exe, err := currentBinary()
	if err != nil {
		return err
	}
	if err := writeIfAbsent(filepath.Join(zedConfigDir(), "tasks.json"), zedTasks(exe)); err != nil {
		return err
	}

	if !ownExtensionInstalled() {
		fmt.Println("\n! The mdrev extension is not installed.")
		fmt.Println(extensionNote)
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

// initProject prepares one project. With the extension installed there is
// nothing to configure for the editor here — Zed starts a server an extension
// declares on its own — so this only installs agent instructions, and falls
// back to borrowing another extension's server slot when ours is missing.
func initProject(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	agentDocs := fs.String("agent-docs", "", "instructions for coding agents: agents | skill | both | none")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if !ownExtensionInstalled() {
		exe, err := currentBinary()
		if err != nil {
			return err
		}
		fmt.Println("The mdrev extension is not installed; falling back to the " +
			hostExtension + " server slot for this project.")
		if err := writeIfAbsent(filepath.Join(".zed", "settings.json"), zedSettings(exe)); err != nil {
			return err
		}
		if !hostExtensionInstalled() {
			fmt.Println(extensionNote)
		}
	}

	return setUpAgentDocs(*agentDocs)
}

func currentBinary() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

func zedConfigDir() string {
	return filepath.Join(os.Getenv("HOME"), ".config", "zed")
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

// sameChord keeps the modifiers of a chosen shortcut and swaps the final key,
// so the review bindings stay a family: alt-c and alt-t, or ctrl-alt-k and
// ctrl-alt-t.
func sameChord(key, letter string) string {
	i := strings.LastIndex(key, "-")
	return key[:i+1] + letter
}

func shiftVariant(key string) string {
	if strings.Contains(key, "shift-") {
		return ""
	}
	i := strings.LastIndex(key, "-")
	return key[:i+1] + "shift-" + key[i+1:]
}

func applyKeymap(comment, question string, write bool) error {
	path := filepath.Join(zedConfigDir(), "keymap.json")
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

// zedSettings borrows another extension's server slot, for when ours is not
// installed. "..." keeps the other Markdown servers running next to us.
func zedSettings(exe string) string {
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
		// The thread browser is a full-screen UI, so it gets the centre pane
		// rather than the dock, and takes no selection.
		map[string]any{
			"label":                 threadsTask,
			"command":               exe,
			"args":                  []string{"threads", "$ZED_FILE"},
			"use_new_terminal":      false,
			"allow_concurrent_runs": false,
			"reveal":                "always",
			"reveal_target":         "center",
		},
	})
}

func zedKeymap(comment, question string) string {
	bindings := map[string]any{
		comment:                 []any{"task::Spawn", map[string]any{"task_name": commentTask}},
		sameChord(comment, "t"): []any{"task::Spawn", map[string]any{"task_name": threadsTask}},
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
