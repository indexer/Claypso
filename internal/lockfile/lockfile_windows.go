//go:build windows

package lockfile

import "os"

func Lock(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
}

func Unlock(f *os.File) error {
	return f.Close()
}
