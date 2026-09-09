package mrsf

import (
	"os"
	"os/exec"
	"strings"
)

// DefaultAuthor names whoever is at the keyboard. A thread is only readable
// later if it says who asked and who answered, so this falls back rather than
// leaving the field empty.
func DefaultAuthor() string {
	if out, err := exec.Command("git", "config", "user.name").Output(); err == nil {
		if name := strings.TrimSpace(string(out)); name != "" {
			return name
		}
	}
	return os.Getenv("USER")
}
