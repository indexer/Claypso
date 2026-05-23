//go:build !linux && !darwin

package keychain

import "errors"

func store(passphrase []byte) error {
	return errors.New("keychain not supported on this platform")
}

func retrieve() ([]byte, error) {
	return nil, errors.New("keychain not supported on this platform")
}

func forget() error {
	return errors.New("keychain not supported on this platform")
}

func available() bool {
	return false
}
