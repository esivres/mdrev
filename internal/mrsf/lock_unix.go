//go:build unix

package mrsf

import (
	"os"

	"golang.org/x/sys/unix"
)

// flock is used rather than a POSIX fcntl lock because fcntl locks are held per
// process: the language server's request handler and its watcher goroutine
// would not be locked against each other at all.
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
