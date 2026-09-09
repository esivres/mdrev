package mrsf

import (
	"fmt"
	"os"
	"time"
)

// lockTimeout bounds how long a writer waits. A keystroke in the editor or the
// browser must not hang on a lock somebody else is holding; failing loudly
// after a moment is better than a frozen UI.
const lockTimeout = 2 * time.Second

// Update applies fn to a document's review under an exclusive lock and writes
// the result. Every write goes through here: load and save are not separately
// callable, so the lock cannot be forgotten by the next thing that needs to
// change a comment.
//
// The lock does not extend to tools that know nothing about it — the reference
// mrsf CLI, or a hand edit — and advisory locks are unreliable on network and
// file-syncing filesystems. It covers concurrent mdrev processes, which is what
// an agent writing while a human resolves actually produces.
func Update(document string, fn func(*Sidecar) error) error {
	release, err := lockSidecar(document)
	if err != nil {
		return err
	}
	defer release()

	sc, err := LoadOrCreate(document)
	if err != nil {
		return err
	}
	if err := fn(sc); err != nil {
		return err
	}
	return sc.save()
}

// lockSidecar takes the lock on a file beside the sidecar rather than on the
// sidecar itself: Save replaces that file by rename, so a lock held on its
// inode would be orphaned the moment anyone wrote, and two writers would each
// hold an exclusive lock on a different file. The lock file is never removed —
// deleting it is the classic race where one process unlinks the file another
// has just opened.
func lockSidecar(document string) (func(), error) {
	path := Path(document) + ".lock"
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		// A read-only directory should not stop a review being read, and there
		// is nobody to contend with if nobody can write.
		return func() {}, nil
	}

	deadline := time.Now().Add(lockTimeout)
	for {
		locked, err := tryLock(f)
		if err != nil {
			f.Close()
			return nil, fmt.Errorf("locking %s: %w", path, err)
		}
		if locked {
			return func() {
				unlock(f)
				f.Close()
			}, nil
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("another mdrev is writing %s (waited %s)", Path(document), lockTimeout)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
