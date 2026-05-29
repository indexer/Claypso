//go:build windows

package main

// hardenProcess is a no-op on Windows: there is no per-process core-dump or
// ptrace lever to disable at startup. Windows Error Reporting crash dumps are
// governed by separate, system-level policy rather than something the process
// sets for itself.
func hardenProcess() error { return nil }
