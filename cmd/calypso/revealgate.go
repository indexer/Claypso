package main

import (
	"fmt"
	"os"

	"golang.org/x/term"
)

// unattendedRevealEnv must be set to "1" for reveal-class operations to run
// without an interactive terminal (CI pipelines). Headless AI agents should
// not be given it; together with the vault's lockdown flag this keeps real
// values out of agent transcripts by default.
const unattendedRevealEnv = "CALYPSO_UNATTENDED"

// guardReveal gates operations that print or write real secret values
// (get --reveal, diff --reveal, drift --reveal, plain pull, exports).
// lockdown is the vault's Lockdown flag; allowUnattended is its
// LockdownUnattendedOK policy (CI exemption). Callers without an open vault
// pass false, false and get only the TTY check.
func guardReveal(lockdown, allowUnattended bool, op string) error {
	return guardRevealWith(lockdown, allowUnattended, term.IsTerminal(int(os.Stdin.Fd())), os.Getenv(unattendedRevealEnv), op) //nolint:gosec // fd fits in int on supported platforms
}

// guardUnsafeOutput is intentionally stricter than guardReveal: unattended
// mode never exempts an operation that disables transcript scrubbing.
func guardUnsafeOutput(lockdown, strict bool, op string) error {
	return guardUnsafeOutputWith(lockdown, strict, term.IsTerminal(int(os.Stdin.Fd())), op) //nolint:gosec // fd fits in int on supported platforms
}

func guardUnsafeOutputWith(lockdown, strict, tty bool, op string) error {
	if strict {
		return fmt.Errorf("%s is blocked: vault is in agent strict mode", op)
	}
	if lockdown {
		return fmt.Errorf("%s is blocked: vault is in lockdown mode", op)
	}
	if !tty {
		return fmt.Errorf("%s needs an interactive terminal; unattended use is never allowed", op)
	}
	return nil
}

// guardRevealWith is the testable core of guardReveal.
func guardRevealWith(lockdown, allowUnattended, tty bool, unattendedVal, op string) error {
	if lockdown {
		if allowUnattended && unattendedVal == "1" {
			return nil // vault was locked with --allow-unattended: explicit CI contexts stay exempt
		}
		if allowUnattended {
			return fmt.Errorf("%s is blocked: vault is in lockdown mode (unattended pipelines are exempt — set %s=1); run `calypso lockdown off` in an interactive terminal to lift it", op, unattendedRevealEnv)
		}
		return fmt.Errorf("%s is blocked: vault is in lockdown mode; run `calypso lockdown off` in an interactive terminal", op)
	}
	if tty || unattendedVal == "1" {
		return nil
	}
	return fmt.Errorf("%s reveals real values and needs an interactive terminal; set %s=1 to allow unattended use (CI)", op, unattendedRevealEnv)
}
