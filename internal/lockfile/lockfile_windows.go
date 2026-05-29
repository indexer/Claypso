//go:build windows

// Package lockfile provides advisory file locking. On Windows it uses
// LockFileEx, the counterpart to the unix build's flock(2): an exclusive,
// non-blocking byte-range lock (LOCKFILE_EXCLUSIVE_LOCK |
// LOCKFILE_FAIL_IMMEDIATELY) that fails immediately when another handle holds
// it, matching flock(LOCK_EX|LOCK_NB). Locks are owned by the file handle, so
// a second OpenFile in the same or another process contends as expected.
package lockfile

import (
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func Lock(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	// Lock a single byte at offset 0. Windows permits locking a range beyond
	// EOF, so this works even though the lock file is empty.
	if err := windows.LockFileEx(
		windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, new(windows.Overlapped),
	); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func Unlock(f *os.File) error {
	unlockErr := windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, new(windows.Overlapped))
	closeErr := f.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
