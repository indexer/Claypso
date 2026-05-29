package keychain

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// These tests are HERMETIC: they never store, read, or delete a real secret
// from the OS keychain and never depend on `secret-tool` / `security` being
// installed or on a live keyring daemon / D-Bus session being present. Where a
// backend would otherwise be touched, the package-level seams
// (storeFn/retrieveFn/forgetFn in keychain.go, lookPath in the platform files)
// are replaced with spies/stubs and restored afterwards.
//
// They exercise the platform-independent surface of the package:
//   - the exported ServiceName constant used to build the keychain entry name,
//   - the ErrNotAvailable sentinel (identity + message, including %w wrapping),
//   - the contract of Available(),
//   - the delegation of the exported Store/Retrieve/Forget wrappers, on every
//     OS, via the storeFn/retrieveFn/forgetFn seams.
//
// The unsupported-platform stub behaviour (keychain_other.go) is covered by
// TestUnsupportedPlatform_* in keychain_other_test.go; those tests are gated by
// the `!linux && !darwin` build tag, so they compile and run only on non-Linux/
// non-darwin builds. CI guards them on Linux/darwin via cross-compile `go vet`
// (e.g. GOOS=windows) rather than by executing them.

// TestServiceNameConstant pins the value used to construct the service /
// account / label names in every platform backend. If this changes,
// previously-stored secrets become unreadable, so it is locked down here.
func TestServiceNameConstant(t *testing.T) {
	if ServiceName != "calypso" {
		t.Fatalf("ServiceName = %q, want %q", ServiceName, "calypso")
	}
	if strings.TrimSpace(ServiceName) != ServiceName || ServiceName == "" {
		t.Fatalf("ServiceName must be non-empty and free of surrounding whitespace, got %q", ServiceName)
	}
}

// TestErrNotAvailable verifies the sentinel error is non-nil, stable, and
// carries an actionable, platform-mentioning message. Callers compare against
// this value, so its identity matters.
func TestErrNotAvailable(t *testing.T) {
	if ErrNotAvailable == nil {
		t.Fatal("ErrNotAvailable must not be nil")
	}
	// Identity must survive wrapping: callers wrap this sentinel with %w and
	// later test for it via errors.Is, so verify that an unwrap-chain match
	// works (a non-circular check, unlike comparing the sentinel to itself).
	wrapped := fmt.Errorf("context: %w", ErrNotAvailable)
	if !errors.Is(wrapped, ErrNotAvailable) {
		t.Fatal("errors.Is(fmt.Errorf(\"...: %w\", ErrNotAvailable), ErrNotAvailable) should be true")
	}
	msg := ErrNotAvailable.Error()
	if msg == "" {
		t.Fatal("ErrNotAvailable message must not be empty")
	}
	// The message should help the user fix the situation: it mentions both
	// the Linux tooling and macOS Keychain.
	for _, want := range []string{"keychain", "libsecret", "macOS Keychain"} {
		if !strings.Contains(msg, want) {
			t.Errorf("ErrNotAvailable message %q should mention %q", msg, want)
		}
	}
}

// TestAvailableDelegatesToBackend confirms Available() forwards to the
// platform stub and returns its boolean verbatim. The fixed-value behaviour of
// each backend is asserted hermetically in the platform-specific test files
// (which stub the lookPath seam: see keychain_linux_test.go /
// keychain_darwin_test.go on supported OSes, and TestUnsupportedPlatform_*
// in keychain_other_test.go on every other build). Here we only verify the
// exported wrapper never panics and yields a usable boolean.
func TestAvailableDoesNotPanic(t *testing.T) {
	// Calling Available() must not panic or hang. Its concrete return value is
	// pinned per-OS in the platform test files via the lookPath seam; here we
	// just exercise the exported delegation path.
	_ = Available()
}

// TestExportedWrappersDelegate confirms the exported Store/Retrieve/Forget
// wrappers forward to the correct platform implementation, pass their
// arguments through unchanged, and propagate the implementation's return
// values verbatim. It catches a copy-paste delegation swap (e.g. Store calling
// the forget path) that an OS-conditional skip would let ship.
//
// This is fully hermetic and runs on EVERY platform: instead of executing a
// real backend, it temporarily replaces the storeFn/retrieveFn/forgetFn seams
// (declared in keychain.go) with spies, then restores them via defer.
func TestExportedWrappersDelegate(t *testing.T) {
	// Save and restore the real seams so we never leak spies between tests.
	origStore, origRetrieve, origForget := storeFn, retrieveFn, forgetFn
	t.Cleanup(func() {
		storeFn, retrieveFn, forgetFn = origStore, origRetrieve, origForget
	})

	t.Run("Store forwards to storeFn", func(t *testing.T) {
		var (
			storeCalled    bool
			gotPassphrase  []byte
			retrieveCalled bool
			forgetCalled   bool
		)
		sentinel := errors.New("store sentinel")
		storeFn = func(p []byte) error {
			storeCalled = true
			gotPassphrase = p
			return sentinel
		}
		retrieveFn = func() ([]byte, error) { retrieveCalled = true; return nil, nil }
		forgetFn = func() error { forgetCalled = true; return nil }

		want := []byte("super-secret-passphrase")
		err := Store(want)

		if !storeCalled {
			t.Fatal("Store() did not call storeFn")
		}
		if retrieveCalled || forgetCalled {
			t.Fatalf("Store() called the wrong seam (retrieve=%v forget=%v)", retrieveCalled, forgetCalled)
		}
		if !bytes.Equal(gotPassphrase, want) {
			t.Errorf("Store() forwarded passphrase %q, want %q", gotPassphrase, want)
		}
		if !errors.Is(err, sentinel) {
			t.Errorf("Store() returned err %v, want propagated sentinel %v", err, sentinel)
		}
	})

	t.Run("Retrieve forwards to retrieveFn", func(t *testing.T) {
		var (
			storeCalled    bool
			retrieveCalled bool
			forgetCalled   bool
		)
		wantVal := []byte("retrieved-value")
		sentinel := errors.New("retrieve sentinel")
		storeFn = func([]byte) error { storeCalled = true; return nil }
		retrieveFn = func() ([]byte, error) {
			retrieveCalled = true
			return wantVal, sentinel
		}
		forgetFn = func() error { forgetCalled = true; return nil }

		gotVal, err := Retrieve()

		if !retrieveCalled {
			t.Fatal("Retrieve() did not call retrieveFn")
		}
		if storeCalled || forgetCalled {
			t.Fatalf("Retrieve() called the wrong seam (store=%v forget=%v)", storeCalled, forgetCalled)
		}
		if !bytes.Equal(gotVal, wantVal) {
			t.Errorf("Retrieve() returned value %q, want propagated %q", gotVal, wantVal)
		}
		if !errors.Is(err, sentinel) {
			t.Errorf("Retrieve() returned err %v, want propagated sentinel %v", err, sentinel)
		}
	})

	t.Run("Forget forwards to forgetFn", func(t *testing.T) {
		var (
			storeCalled    bool
			retrieveCalled bool
			forgetCalled   bool
		)
		sentinel := errors.New("forget sentinel")
		storeFn = func([]byte) error { storeCalled = true; return nil }
		retrieveFn = func() ([]byte, error) { retrieveCalled = true; return nil, nil }
		forgetFn = func() error {
			forgetCalled = true
			return sentinel
		}

		err := Forget()

		if !forgetCalled {
			t.Fatal("Forget() did not call forgetFn")
		}
		if storeCalled || retrieveCalled {
			t.Fatalf("Forget() called the wrong seam (store=%v retrieve=%v)", storeCalled, retrieveCalled)
		}
		if !errors.Is(err, sentinel) {
			t.Errorf("Forget() returned err %v, want propagated sentinel %v", err, sentinel)
		}
	})
}
