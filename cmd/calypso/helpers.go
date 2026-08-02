package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"
)

const (
	minPassphraseLen = 8

	// canonicalPassphraseEnv is the supported name for the unattended
	// passphrase. legacyPassphraseEnv is the pre-rename name (the tool used to
	// be "envhub"); it is still honored for backward compatibility with a
	// deprecation notice.
	canonicalPassphraseEnv = "CALYPSO_PASSPHRASE"
	legacyPassphraseEnv    = "ENVHUB_PASSPHRASE" //nolint:gosec // env var name, not a credential
)

// passphraseEnvNames lists the environment variables consulted for an
// unattended passphrase, in priority order.
var passphraseEnvNames = []string{canonicalPassphraseEnv, legacyPassphraseEnv}

// passphraseFromEnv returns the unattended passphrase and the name of the
// variable it came from. found is false when none is set.
func passphraseFromEnv() (value, name string, found bool) {
	for _, n := range passphraseEnvNames {
		if v := os.Getenv(n); v != "" {
			return v, n, true
		}
	}
	return "", "", false
}

// unattended reports whether a passphrase env var is set — i.e. calypso is
// running non-interactively, so confirmation prompts auto-accept.
func unattended() bool {
	_, _, ok := passphraseFromEnv()
	return ok
}

// clearBytes securely zeroes a byte slice in memory.
func clearBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// promptPassphrase reads a passphrase without echo. Returns (pw, fromEnv, err).
// fromEnv is true when the password came from ENVHUB_PASSPHRASE.
func promptPassphrase(prompt string) ([]byte, bool, error) {
	fmt.Fprint(os.Stderr, prompt)
	if p, name, ok := passphraseFromEnv(); ok {
		fmt.Fprintf(os.Stderr, "(using %s)\n", name)
		if name == legacyPassphraseEnv {
			fmt.Fprintf(os.Stderr, "warning: %s is deprecated; rename it to %s.\n",
				legacyPassphraseEnv, canonicalPassphraseEnv)
		}
		return []byte(p), true, nil
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	type result struct {
		pw  []byte
		err error
	}
	resCh := make(chan result, 1)
	go func() {
		pw, err := term.ReadPassword(int(os.Stdin.Fd())) //nolint:gosec // fd fits in int on supported platforms
		resCh <- result{pw, err}
	}()

	select {
	case res := <-resCh:
		fmt.Fprintln(os.Stderr)
		if res.err != nil {
			return nil, false, fmt.Errorf("reading passphrase: %w", res.err)
		}
		return res.pw, false, nil
	case <-sigCh:
		fmt.Fprintln(os.Stderr, "\n^C")
		os.Exit(130)
		return nil, false, nil // unreachable
	}
}

// readNewPassphrase prompts twice and confirms they match.
func readNewPassphrase() ([]byte, error) {
	for {
		pw, fromEnv, err := promptPassphrase("Choose a master passphrase: ")
		if err != nil {
			return nil, err
		}
		if len(pw) < minPassphraseLen {
			clearBytes(pw)
			return nil, fmt.Errorf("passphrase must be at least %d characters", minPassphraseLen)
		}
		confirm, _, err := promptPassphrase("Confirm passphrase: ")
		if err != nil {
			clearBytes(pw)
			return nil, err
		}
		if bytes.Equal(pw, confirm) {
			clearBytes(confirm)
			return pw, nil
		}
		clearBytes(pw)
		clearBytes(confirm)
		if fromEnv {
			return nil, fmt.Errorf("passphrases do not match")
		}
		fmt.Fprintln(os.Stderr, "Passphrases do not match. Try again.")
	}
}

// confirmYesNo prompts the user. In unattended mode (ENVHUB_PASSPHRASE set),
// returns true immediately.
func confirmYesNo(out io.Writer, prompt string, defaultYes bool) (bool, error) {
	if unattended() {
		return true, nil
	}
	suffix := "[y/N]"
	if defaultYes {
		suffix = "[Y/n]"
	}
	fmt.Fprintf(out, "%s %s: ", prompt, suffix)
	r := bufio.NewReader(os.Stdin)
	answer, err := r.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("reading response: %w", err)
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer == "" {
		return defaultYes, nil
	}
	return answer == "y" || answer == "yes", nil
}

// warnBackupErr emits a stderr warning if the most recent Save's auto-backup
// failed. The Save itself succeeded — this just lets the user know the new
// backup wasn't written (disk full, perms, etc.).
func warnBackupErr(v interface{ LastBackupErr() error }) {
	if err := v.LastBackupErr(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: auto-backup failed: %v\n", err)
	}
}

// vaultSaver is the narrow interface saveAndWarn needs. *vault.Vault satisfies it.
type vaultSaver interface {
	Save(ctx context.Context, path string, passphrase []byte) error
	LastBackupErr() error
}

// saveAndWarn writes the vault and, if Save succeeded, surfaces any
// auto-backup warning to stderr. Use this in CLI commands instead of
// calling v.Save directly so backup problems aren't silent.
func saveAndWarn(ctx context.Context, v vaultSaver, pw []byte) error {
	if err := v.Save(ctx, vaultPath, pw); err != nil {
		return err
	}
	warnBackupErr(v)
	return nil
}

// maskFixed is the fixed-width mask used for the default (shareable) view. It
// deliberately reveals neither the value's length nor any of its characters.
const maskFixed = "********"

// maybeMask renders a value for display. With reveal it returns the value
// verbatim. Otherwise it masks: by default to a fixed width that leaks nothing,
// or — with hint — showing the first and last two characters as an
// identification aid (which does leak length and 4 chars, so it's opt-in).
func maybeMask(val string, reveal, hint bool) string {
	if reveal {
		return val
	}
	if !hint {
		return maskFixed
	}
	if len(val) <= 4 {
		return "****"
	}
	n := len(val)
	mid := n - 4
	b := make([]byte, n)
	copy(b[:2], val[:2])
	for i := 2; i < 2+mid; i++ {
		b[i] = '*'
	}
	copy(b[2+mid:], val[n-2:])
	return string(b)
}

// stripSensitiveEnv removes any passphrase env var (CALYPSO_PASSPHRASE and the
// legacy ENVHUB_PASSPHRASE) from the environment slice before passing it to
// child processes, so a `pull -- cmd` never leaks the master passphrase.
func stripSensitiveEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		if isPassphraseAssignment(kv) || strings.HasPrefix(kv, unattendedRevealEnv+"=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func isPassphraseAssignment(kv string) bool {
	for _, n := range passphraseEnvNames {
		if strings.HasPrefix(kv, n+"=") {
			return true
		}
	}
	// The rekey rotation variable is just as much a credential as the
	// current passphrase — never pass it to `pull -- cmd` children either.
	return strings.HasPrefix(kv, newPassphraseEnv+"=")
}
