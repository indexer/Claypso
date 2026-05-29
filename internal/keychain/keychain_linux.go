//go:build linux

package keychain

import (
	"bytes"
	"os"
	"os/exec"
)

func store(passphrase []byte) error {
	cmd := exec.Command("secret-tool", "store", "--label=calypso vault passphrase", "app", ServiceName)
	cmd.Stdin = bytes.NewReader(passphrase)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func retrieve() ([]byte, error) {
	out, err := exec.Command("secret-tool", "lookup", "app", ServiceName).Output()
	if err != nil {
		return nil, err
	}
	return bytes.TrimRight(out, "\n"), nil
}

func forget() error {
	return exec.Command("secret-tool", "clear", "app", ServiceName).Run()
}

// lookPath is a seam over exec.LookPath so tests can pin the exact binary name
// available() probes for without depending on what is installed on PATH.
var lookPath = exec.LookPath

func available() bool {
	_, err := lookPath("secret-tool")
	return err == nil
}
