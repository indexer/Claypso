//go:build darwin

package keychain

import (
	"os/exec"
	"testing"
)

// TestDarwinAvailableUsesSecurity pins the available() contract on macOS
// non-circularly: it stubs the lookPath seam (declared in keychain_darwin.go)
// so the test never inspects the real PATH or runs `security`. It asserts that
// available() probes for the EXACT binary name "security" and maps the lookup
// result to the right boolean in both directions.
func TestDarwinAvailableUsesSecurity(t *testing.T) {
	origLookPath := lookPath
	t.Cleanup(func() { lookPath = origLookPath })

	t.Run("present on PATH -> available", func(t *testing.T) {
		var gotName string
		lookPath = func(file string) (string, error) {
			gotName = file
			return "/usr/bin/security", nil
		}
		if !available() {
			t.Error("available() = false when security resolves, want true")
		}
		if gotName != "security" {
			t.Errorf("available() looked up %q, want %q", gotName, "security")
		}
	})

	t.Run("absent from PATH -> unavailable", func(t *testing.T) {
		var gotName string
		lookPath = func(file string) (string, error) {
			gotName = file
			return "", exec.ErrNotFound
		}
		if available() {
			t.Error("available() = true when security is missing, want false")
		}
		if gotName != "security" {
			t.Errorf("available() looked up %q, want %q", gotName, "security")
		}
	})
}

// TestDarwinAvailableExportedMatchesSeam checks the exported Available()
// wrapper reflects the stubbed seam in both directions, again without touching
// PATH.
func TestDarwinAvailableExportedMatchesSeam(t *testing.T) {
	origLookPath := lookPath
	t.Cleanup(func() { lookPath = origLookPath })

	lookPath = func(string) (string, error) { return "/usr/bin/security", nil }
	if !Available() {
		t.Error("Available() = false when security resolves, want true")
	}

	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	if Available() {
		t.Error("Available() = true when security is missing, want false")
	}
}

// TestDarwinStoreRetrieveForgetNeedLiveBackend documents that the actual
// store/retrieve/forget round-trip is intentionally not exercised here: it
// would shell out to `security`, mutating the real login Keychain (and could
// trigger an interactive unlock prompt). We keep CI hermetic by skipping it.
// (Delegation of the exported wrappers is covered cross-platform in
// keychain_test.go.)
func TestDarwinStoreRetrieveForgetNeedLiveBackend(t *testing.T) {
	t.Skip("store/retrieve/forget mutate the real macOS login Keychain; " +
		"skipped to keep the test hermetic and avoid writing real secrets")
}
