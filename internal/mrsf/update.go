package mrsf

import (
	"fmt"
	"os"
	"time"
)

// Bounded so a keystroke never hangs on somebody else's lock.
const lockTimeout = 2 * time.Second

// Update applies fn to a document's review under an exclusive lock. It is the
// only way to write one, so the lock cannot be forgotten.
//
// The lock is advisory: it binds mdrev processes, not other tools, and is
// unreliable on network and file-syncing filesystems.
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

// The lock lives beside the sidecar, not on it: save replaces the sidecar by
// rename, which would orphan a lock held on its inode. The lock file is never
// removed — unlinking it races with whoever has it open.
func lockSidecar(document string) (func(), error) {
	path := Path(document) + ".lock"
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		// Nobody can write here, so nobody can contend.
		return func() {}, nil //nolint:nilerr
	}

	deadline := time.Now().Add(lockTimeout)
	for {
		locked, err := tryLock(f)
		if err != nil {
			_ = f.Close()
			return nil, fmt.Errorf("locking %s: %w", path, err)
		}
		if locked {
			return func() {
				_ = unlock(f)
				_ = f.Close()
			}, nil
		}
		if time.Now().After(deadline) {
			_ = f.Close()
			return nil, fmt.Errorf("another mdrev is writing %s (waited %s)", Path(document), lockTimeout)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
