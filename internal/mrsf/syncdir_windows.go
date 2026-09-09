//go:build windows

package mrsf

// Windows refuses to open a directory as a file, so there is nothing to sync;
// MoveFileEx already commits the replacement.
func syncDir(string) error { return nil }
