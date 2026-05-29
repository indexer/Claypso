//go:build linux

package keychain

import (
	"os/exec"
	"testing"
)

// TestLinuxAvailableUsesSecretTool pins the available() contract on Linux
// non-circularly: it stubs the lookPath seam (declared in keychain_linux.go)
// so the test never inspects the real PATH or runs `secret-tool`. It asserts
// that available() probes for the EXACT binary name "secret-tool" and maps the
// lookup result to the right boolean in both directions.
func TestLinuxAvailableUsesSecretTool(t *testing.T) {
	origLookPath := lookPath
	t.Cleanup(func() { lookPath = origLookPath })

	t.Run("present on PATH -> available", func(t *testing.T) {
		var gotName string
		lookPath = func(file string) (string, error) {
			gotName = file
			return "/usr/bin/secret-tool", nil
		}
		if !available() {
			t.Error("available() = false when secret-tool resolves, want true")
		}
		if gotName != "secret-tool" {
			t.Errorf("available() looked up %q, want %q", gotName, "secret-tool")
		}
	})

	t.Run("absent from PATH -> unavailable", func(t *testing.T) {
		var gotName string
		lookPath = func(file string) (string, error) {
			gotName = file
			return "", exec.ErrNotFound
		}
		if available() {
			t.Error("available() = true when secret-tool is missing, want false")
		}
		if gotName != "secret-tool" {
			t.Errorf("available() looked up %q, want %q", gotName, "secret-tool")
		}
	})
}

// TestLinuxAvailableExportedMatchesSeam checks the exported Available() wrapper
// reflects the stubbed seam in both directions, again without touching PATH.
func TestLinuxAvailableExportedMatchesSeam(t *testing.T) {
	origLookPath := lookPath
	t.Cleanup(func() { lookPath = origLookPath })

	lookPath = func(string) (string, error) { return "/usr/bin/secret-tool", nil }
	if !Available() {
		t.Error("Available() = false when secret-tool resolves, want true")
	}

	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	if Available() {
		t.Error("Available() = true when secret-tool is missing, want false")
	}
}

// TestLinuxStoreRetrieveForgetNeedLiveBackend documents that the actual
// store/retrieve/forget round-trip is intentionally not exercised here: it
// would shell out to `secret-tool`, which requires a real libsecret backend
// and a D-Bus session, and would write a credential into the developer's /
// CI runner's keyring. We keep CI hermetic by skipping it. (Delegation of the
// exported wrappers is covered cross-platform in keychain_test.go.)
func TestLinuxStoreRetrieveForgetNeedLiveBackend(t *testing.T) {
	t.Skip("store/retrieve/forget require a live libsecret/D-Bus keyring; " +
		"skipped to keep the test hermetic and avoid writing real secrets")
}
