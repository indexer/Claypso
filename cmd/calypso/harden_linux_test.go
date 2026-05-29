//go:build linux

package main

import (
	"testing"

	"golang.org/x/sys/unix"
)

// TestHardenProcessLinux verifies hardenProcess actually disables core dumps
// and marks the process non-dumpable, then restores both so other tests in the
// package run with normal process state.
func TestHardenProcessLinux(t *testing.T) {
	var orig unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_CORE, &orig); err == nil {
		t.Cleanup(func() { _ = unix.Setrlimit(unix.RLIMIT_CORE, &orig) })
	}
	t.Cleanup(func() { _, _ = unix.PrctlRetInt(unix.PR_SET_DUMPABLE, 1, 0, 0, 0) })

	if err := hardenProcess(); err != nil {
		t.Fatalf("hardenProcess: %v", err)
	}

	var lim unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_CORE, &lim); err != nil {
		t.Fatalf("getrlimit RLIMIT_CORE: %v", err)
	}
	if lim.Cur != 0 {
		t.Errorf("RLIMIT_CORE soft limit = %d, want 0 (core dumps disabled)", lim.Cur)
	}

	d, err := unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0)
	if err != nil {
		t.Fatalf("PR_GET_DUMPABLE: %v", err)
	}
	if d != 0 {
		t.Errorf("dumpable flag = %d, want 0 (non-dumpable)", d)
	}
}
