package main

import (
	"bufio"
	"bytes"
	"fmt"
	"golang.org/x/term"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

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
	if p := os.Getenv("ENVHUB_PASSPHRASE"); p != "" {
		fmt.Fprintln(os.Stderr, "(using ENVHUB_PASSPHRASE)")
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
		pw, err := term.ReadPassword(int(os.Stdin.Fd()))
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
	if os.Getenv("ENVHUB_PASSPHRASE") != "" {
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

// maybeMask shows first 2 + last 2 chars unless reveal is true.
func maybeMask(val string, reveal bool) string {
	if reveal {
		return val
	}
	if len(val) <= 4 {
		return "****"
	}
	var b strings.Builder
	middle := len(val) - 4
	b.Grow(len(val))
	b.WriteString(val[:2])
	for i := 0; i < middle; i++ {
		b.WriteByte('*')
	}
	b.WriteString(val[len(val)-2:])
	return b.String()
}
