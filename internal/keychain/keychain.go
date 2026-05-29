// Package keychain provides OS-level secure storage for the vault master
// passphrase via the system keyring (Keychain on macOS, libsecret on Linux).
package keychain

import "fmt"

// ServiceName is the key under which the passphrase is stored.
const ServiceName = "calypso"

// ErrNotAvailable is returned when no supported keychain backend is found.
var ErrNotAvailable = fmt.Errorf("no keychain backend available; install libsecret-tools (Linux) or use macOS Keychain")

// storeFn, retrieveFn and forgetFn are indirection seams over the
// platform-specific implementations. The exported wrappers route through these
// vars so tests can substitute spies and verify the delegation contract on
// every OS (see TestExportedWrappersDelegate). Production code never reassigns
// them.
var (
	storeFn    = store
	retrieveFn = retrieve
	forgetFn   = forget
)

// Store saves the passphrase in the OS keychain.
func Store(passphrase []byte) error {
	return storeFn(passphrase)
}

// Retrieve fetches the passphrase from the OS keychain.
func Retrieve() ([]byte, error) {
	return retrieveFn()
}

// Forget removes the passphrase from the OS keychain.
func Forget() error {
	return forgetFn()
}

// Available returns true if a keychain backend is usable.
func Available() bool {
	return available()
}
