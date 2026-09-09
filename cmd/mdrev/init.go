package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const marksmanNote = `Zed не даёт зарегистрировать свой language server через настройки:
сервер объявляется только расширением. Поэтому mdrev подменяет собой
расширение marksman — оно объявляет себя сервером для Markdown, а мы
переопределяем его бинарник. Установи Marksman из панели расширений Zed.`

func initProject(args []string) error {
	writeKeymap := false
	for _, a := range args {
		if a == "--keymap" {
			writeKeymap = true
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(".zed", 0o755); err != nil {
		return err
	}
	if err := writeIfAbsent(filepath.Join(".zed", "settings.json"), zedSettings(exe)); err != nil {
		return err
	}
	if err := writeIfAbsent(filepath.Join(".zed", "tasks.json"), zedTasks(exe)); err != nil {
		return err
	}

	if !marksmanInstalled() {
		fmt.Println("\n! Расширение Marksman не найдено.")
		fmt.Println(marksmanNote)
	}

	keymapPath := filepath.Join(os.Getenv("HOME"), ".config", "zed", "keymap.json")
	if writeKeymap {
		if err := writeIfAbsent(keymapPath, zedKeymap()); err != nil {
			return err
		}
	} else if _, err := os.Stat(keymapPath); err != nil {
		fmt.Println("\nХоткеи в Zed задаются только глобально. Добавь в " + keymapPath + ":")
		fmt.Println(zedKeymap())
	}
	return nil
}

// writeIfAbsent never overwrites: these files usually hold the user's own
// settings, and merging JSON blind is worse than printing what to paste.
func writeIfAbsent(path, content string) error {
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("\n%s уже существует, не трогаю. Нужный фрагмент:\n%s\n", path, content)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return err
	}
	fmt.Println("создан", path)
	return nil
}

func marksmanInstalled() bool {
	_, err := os.Stat(filepath.Join(os.Getenv("HOME"),
		".local/share/zed/extensions/installed/marksman/extension.toml"))
	return err == nil
}

func zedSettings(exe string) string {
	return mustJSON(map[string]any{
		"languages": map[string]any{
			"Markdown": map[string]any{"language_servers": []string{"marksman"}},
		},
		"lsp": map[string]any{
			"marksman": map[string]any{
				"binary": map[string]any{
					"path":                  exe,
					"arguments":             []string{"lsp"},
					"ignore_system_version": true,
				},
			},
		},
	})
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
		task("Комментарий к выделенному"),
		task("Вопрос к выделенному", "--type", "question"),
	})
}

func zedKeymap() string {
	return mustJSON([]any{map[string]any{
		"context": "Editor",
		"bindings": map[string]any{
			"alt-c":       []any{"task::Spawn", map[string]any{"task_name": "Комментарий к выделенному"}},
			"alt-shift-c": []any{"task::Spawn", map[string]any{"task_name": "Вопрос к выделенному"}},
		},
	}})
}

func mustJSON(v any) string {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(b) + "\n"
}
