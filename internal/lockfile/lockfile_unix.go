//go:build !windows

// Package lockfile provides advisory file locking via flock(2).
package lockfile

import (
	"os"
	"path/filepath"
	"syscall"
)

func Lock(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil { //nolint:gosec // fd fits in int on supported platforms
		f.Close()
		return nil, err
	}
	return f, nil
}

func Unlock(f *os.File) error {
	unlockErr := syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:gosec // fd fits in int on supported platforms
	closeErr := f.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
