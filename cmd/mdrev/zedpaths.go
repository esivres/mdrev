package main

import (
	"os"
	"path/filepath"
	"runtime"
)

// Zed keeps its configuration and its data in different places, and both differ
// per platform. Mirrors crates/paths in zed-industries/zed.
func zedConfigDir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "Zed")
	case "darwin":
		return filepath.Join(homeDir(), ".config", "zed")
	default:
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			return filepath.Join(xdg, "zed")
		}
		return filepath.Join(homeDir(), ".config", "zed")
	}
}

func zedDataDir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "Zed")
	case "darwin":
		return filepath.Join(homeDir(), "Library", "Application Support", "Zed")
	default:
		if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
			return filepath.Join(xdg, "zed")
		}
		return filepath.Join(homeDir(), ".local", "share", "zed")
	}
}

func zedExtensionDir(id string) string {
	return filepath.Join(zedDataDir(), "extensions", "installed", id)
}

func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return ""
}
