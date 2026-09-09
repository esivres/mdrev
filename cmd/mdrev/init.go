package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/term"

	"github.com/esivres/mdrev/internal/agentdocs"
	"github.com/esivres/mdrev/internal/tui"
)

// Whose language server slot mdrev borrows when its own extension is missing.
// Markdownlint rather than Marksman: Zed runs several servers per language, so
// Marksman's link navigation is worth keeping alive alongside us.
// The registry requires a language-server-only extension to say so in its id.
const extensionID = "mdrev-language-server"

const hostExtension = "markdownlint"

const extensionNote = `Install it from the Zed extensions panel; Zed then starts mdrev for Markdown
on its own, with no per-project settings.

Until then mdrev borrows the ` + hostExtension + ` extension's server slot, which
means that extension has to be installed instead. Either way other Markdown
servers keep running alongside mdrev, so Marksman stays useful.`

const commentTask = "Comment on selection"
const questionTask = "Question about selection"
const threadsTask = "Review threads"

// Offered interactively; anything else comes from --keys.
var keyPresets = []string{"alt-c", "ctrl-alt-c", "ctrl-alt-shift-c,ctrl-alt-shift-q,ctrl-alt-shift-r"}

// setUpEditor configures Zed once per machine: tasks and shortcuts are global.
func setUpEditor(args []string) error {
	fs := flag.NewFlagSet("setup", flag.ExitOnError)
	keys := fs.String("keys", "", "shortcuts as comment[,question[,threads]], e.g. alt-c or alt-c,alt-shift-c,alt-r")
	writeKeymap := fs.Bool("write-keymap", false, "write the shortcuts into Zed's keymap instead of printing them")
	noKeymap := fs.Bool("no-keymap", false, "skip shortcuts entirely")
	if err := fs.Parse(args); err != nil {
		return err
	}

	exe, err := currentBinary()
	if err != nil {
		return err
	}
	if err := mergeBlock(filepath.Join(zedConfigDir(), "tasks.json"), zedTasks(exe)); err != nil {
		return err
	}

	if !ownExtensionInstalled() {
		fmt.Println("\n! The mdrev extension is not installed.")
		fmt.Println(extensionNote)
	}

	if *noKeymap {
		return nil
	}
	comment, question, threads, err := chooseKeys(*keys)
	if err != nil {
		return err
	}
	if comment == "" {
		return nil
	}
	return applyKeymap(comment, question, threads, *writeKeymap)
}

// initProject prepares one project. Zed starts a server an extension declares
// without any settings, so with our extension installed this only installs
// agent instructions.
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

// So the human does not explain mdrev in every session.
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
	case "none":
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

// An empty comment key means "no shortcuts".
func chooseKeys(flagValue string) (comment, question, threads string, err error) {
	if flagValue != "" {
		return splitKeys(flagValue)
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return splitKeys(keyPresets[0])
	}
	if comment, question, threads, ok := pickKeys(); ok {
		return comment, question, threads, nil
	}

	fmt.Println("\nShortcuts for commenting, asking, and the thread browser:")
	for i, k := range keyPresets {
		c, q, t, _ := splitKeys(k)
		suffix := ""
		if i == 0 {
			suffix = "  (default)"
		}
		fmt.Printf("  %d) %s, %s, %s%s\n", i+1, c, q, t, suffix)
	}
	fmt.Printf("  %d) enter your own\n", len(keyPresets)+1)
	fmt.Printf("  %d) no shortcuts\n", len(keyPresets)+2)
	fmt.Print("> ")

	in := bufio.NewScanner(os.Stdin)
	if !in.Scan() {
		return splitKeys(keyPresets[0])
	}
	choice := strings.TrimSpace(in.Text())
	if choice == "" {
		return splitKeys(keyPresets[0])
	}
	n, convErr := strconv.Atoi(choice)
	if convErr != nil {
		return "", "", "", fmt.Errorf("unknown choice %q", choice)
	}
	switch {
	case n >= 1 && n <= len(keyPresets):
		return splitKeys(keyPresets[n-1])
	case n == len(keyPresets)+1:
		fmt.Println("Shortcuts, comma separated: comment[,question[,threads]]")
		fmt.Print("> ")
		if !in.Scan() {
			return "", "", "", fmt.Errorf("no shortcut given")
		}
		return splitKeys(strings.TrimSpace(in.Text()))
	case n == len(keyPresets)+2:
		return "", "", "", nil
	default:
		return "", "", "", fmt.Errorf("unknown choice %q", choice)
	}
}

// Accepts one to three keys. Given fewer, the rest are derived from the first,
// which keeps them a family: alt-c, alt-shift-c, alt-t.
func splitKeys(value string) (comment, question, threads string, err error) {
	parts := strings.Split(value, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	comment = parts[0]
	if comment == "" {
		return "", "", "", fmt.Errorf("empty shortcut")
	}
	if !strings.Contains(comment, "-") {
		// Deriving from a bare key would produce bare letters and shadow vim.
		return "", "", "", fmt.Errorf("shortcut %q has no modifier; use something like alt-c", comment)
	}

	question, threads = shiftVariant(comment), sameChord(comment, "t")
	if len(parts) > 1 && parts[1] != "" {
		question = parts[1]
	}
	if len(parts) > 2 && parts[2] != "" {
		threads = parts[2]
	}
	return comment, question, threads, nil
}

// pickKeys lets the reader press the combinations rather than spell them out.
// Falls back to the text menu if the screen cannot be drawn.
func pickKeys() (comment, question, threads string, ok bool) {
	defaults, _, _, err := splitKeys(keyPresets[0])
	if err != nil {
		return "", "", "", false
	}
	q, t := shiftVariant(defaults), sameChord(defaults, "t")

	keymapPath := filepath.Join(zedConfigDir(), "keymap.json")
	chosen, save, err := tui.ChooseKeys([]tui.Binding{
		{Label: "Comment on selection", Key: defaults},
		{Label: "Question about selection", Key: q},
		{Label: "Review threads", Key: t},
	}, func(key string) string { return boundTo(keymapPath, key) })
	if err != nil || !save {
		return "", "", "", err == nil && !save
	}
	return chosen[0].Key, chosen[1].Key, chosen[2].Key, true
}

// Keeps the modifiers and swaps the final key, so the bindings stay a family.
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

func applyKeymap(comment, question, threads string, write bool) error {
	path := filepath.Join(zedConfigDir(), "keymap.json")
	if taken := conflicts(path, []string{comment, question, threads}); len(taken) > 0 {
		fmt.Printf("\n! %s already appear in your keymap; ours will take precedence in the editor.\n",
			strings.Join(taken, ", "))
		fmt.Println("  Pick others with --keys comment,question,threads")
	}

	entries := zedKeymap(comment, question, threads)
	if write {
		return mergeBlock(path, entries)
	}
	fmt.Printf("\nZed keymaps are global. Add to %s, or rerun with --write-keymap:\n%s\n", path, entries)
	return nil
}

// Never overwrites: these hold the user's own settings, and merging JSON
// blind is worse than printing what to paste.
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
	_, err := os.Stat(filepath.Join(zedExtensionDir(hostExtension), "extension.toml"))
	return err == nil
}

// Borrows another extension's server slot; "..." keeps the rest running.
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
	_, err := os.Stat(filepath.Join(zedExtensionDir(extensionID), "extension.toml"))
	return err == nil
}

func zedTasks(exe string) string {
	// The selection travels in the environment rather than in an argument: Zed
	// splits an argument on whitespace, so a phrase arrived as several
	// arguments and a paragraph could not be passed at all.
	env := map[string]any{
		"MDREV_FILE":  "$ZED_FILE",
		"MDREV_LINE":  "$ZED_ROW",
		"MDREV_QUOTE": "$ZED_SELECTED_TEXT",
	}
	task := func(label string, extra ...string) map[string]any {
		return map[string]any{
			"label":                 label,
			"command":               exe,
			"args":                  append([]string{"comment"}, extra...),
			"env":                   env,
			"use_new_terminal":      false,
			"allow_concurrent_runs": false,
			"reveal":                "always",
			"reveal_target":         "dock",
		}
	}
	return entriesJSON([]any{
		task(commentTask),
		task(questionTask, "--type", "question"),
		// Takes no selection, and lives in the dock beside the document.
		map[string]any{
			"label":                 threadsTask,
			"command":               exe,
			"args":                  []string{"threads"},
			"env":                   env,
			"use_new_terminal":      false,
			"allow_concurrent_runs": false,
			"reveal":                "always",
			"reveal_target":         "dock",
		},
	})
}

// Bound twice: under "Editor" alone nothing fires in vim's normal or visual
// mode, where the vim layer takes the key first.
// Bound twice: under "Editor" alone nothing fires in vim's normal or visual
// mode, where the vim layer takes the key first.
func zedKeymap(comment, question, threads string) string {
	bindings := map[string]any{
		comment: []any{"task::Spawn", map[string]any{"task_name": commentTask}},
	}
	if question != "" {
		bindings[question] = []any{"task::Spawn", map[string]any{"task_name": questionTask}}
	}
	if threads != "" {
		bindings[threads] = []any{"task::Spawn", map[string]any{"task_name": threadsTask}}
	}

	return entriesJSON([]any{
		map[string]any{"context": "Editor && !VimControl", "bindings": bindings},
		map[string]any{"context": "VimControl && !menu", "bindings": bindings},
	})
}

// entriesJSON renders array elements without the surrounding brackets, so they
// can be spliced into a file that already has some.
func entriesJSON(items []any) string {
	rendered := make([]string, 0, len(items))
	for _, item := range items {
		rendered = append(rendered, strings.TrimSuffix(mustJSON(item), "\n"))
	}
	return strings.Join(rendered, ",\n")
}

func mustJSON(v any) string {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	// Zed's contexts contain "&&", which the default encoder would escape into
	// \u0026 — valid JSON, unreadable config.
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		panic(err)
	}
	return buf.String()
}
