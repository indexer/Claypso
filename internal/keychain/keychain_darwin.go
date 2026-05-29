//go:build darwin

package keychain

import (
	"bytes"
	"os/exec"
)

func store(passphrase []byte) error {
	cmd := exec.Command("security", "add-generic-password",
		"-a", ServiceName,
		"-s", ServiceName,
		"-U",
		"-w", string(passphrase),
	)
	cmd.Env = nil
	return cmd.Run()
}

func retrieve() ([]byte, error) {
	out, err := exec.Command("security", "find-generic-password",
		"-a", ServiceName,
		"-s", ServiceName,
		"-w",
	).Output()
	if err != nil {
		return nil, err
	}
	// `security ... -w` prints the password with a trailing newline; strip it
	// so the retrieved value matches what the user typed (and the Linux path).
	return bytes.TrimRight(out, "\n"), nil
}

func forget() error {
	return exec.Command("security", "delete-generic-password",
		"-a", ServiceName,
		"-s", ServiceName,
	).Run()
}

// lookPath is a seam over exec.LookPath so tests can pin the exact binary name
// available() probes for without depending on what is installed on PATH.
var lookPath = exec.LookPath

func available() bool {
	_, err := lookPath("security")
	return err == nil
}
