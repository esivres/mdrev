//go:build unix

package mrsf

import (
	"os"

	"golang.org/x/sys/unix"
)

// flock, not fcntl: fcntl locks are per process, so the language server's
// handler and its watcher goroutine would not be separated at all.
func tryLock(f *os.File) (bool, error) {
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	switch err {
	case nil:
		return true, nil
	case unix.EWOULDBLOCK:
		return false, nil
	default:
		return false, err
	}
}

func unlock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}
