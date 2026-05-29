//go:build !windows && !linux

package main

import "golang.org/x/sys/unix"

// hardenProcess disables core dumps so a crash can't persist in-memory secrets
// to disk. PR_SET_DUMPABLE is Linux-only; on macOS and the BSDs the core-dump
// rlimit is the portable lever. Best-effort — lowering our own limit never
// needs privileges. (mlock is intentionally not used; see the linux build.)
func hardenProcess() error {
	return unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{Cur: 0, Max: 0})
}
