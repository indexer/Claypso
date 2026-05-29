//go:build !linux && !darwin

package keychain

import (
	"strings"
	"testing"
)

// These tests cover the unsupported-platform stub (keychain_other.go). They
// are fully hermetic: the stub never shells out to any tool, so every code
// path can be exercised directly with no backend.

func TestUnsupportedPlatform_Store(t *testing.T) {
	err := store([]byte("secret"))
	if err == nil {
		t.Fatal("store() = nil, want unsupported-platform error")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("store() error = %q, want it to mention %q", err.Error(), "not supported")
	}
}

func TestUnsupportedPlatform_Retrieve(t *testing.T) {
	got, err := retrieve()
	if err == nil {
		t.Fatal("retrieve() = nil error, want unsupported-platform error")
	}
	if got != nil {
		t.Errorf("retrieve() value = %v, want nil on unsupported platform", got)
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("retrieve() error = %q, want it to mention %q", err.Error(), "not supported")
	}
}

func TestUnsupportedPlatform_Forget(t *testing.T) {
	err := forget()
	if err == nil {
		t.Fatal("forget() = nil, want unsupported-platform error")
	}
	if !strings.Contains(err.Error(), "not supported") {
		t.Errorf("forget() error = %q, want it to mention %q", err.Error(), "not supported")
	}
}

func TestUnsupportedPlatform_Available(t *testing.T) {
	if available() {
		t.Error("available() = true on unsupported platform, want false")
	}
}
