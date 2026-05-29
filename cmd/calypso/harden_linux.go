//go:build linux

package main

import "golang.org/x/sys/unix"

// hardenProcess reduces the chance that in-memory secrets leak to disk. It
// marks the process non-dumpable (no core dump, and an unprivileged attacker
// can't ptrace it or read /proc/<pid>/mem) and sets the core-dump size limit to
// zero as belt-and-suspenders. Both are best-effort; lowering our own limits
// never requires privileges.
//
// We deliberately do NOT mlockall: Argon2id allocates 128 MiB per derivation,
// so under the common (low) RLIMIT_MEMLOCK, MCL_FUTURE locking would make that
// allocation fail and break key derivation. Robust anti-swap protection needs
// off-heap guarded memory (memguard), which is out of scope here.
func hardenProcess() error {
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return err
	}
	return unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{Cur: 0, Max: 0})
}
