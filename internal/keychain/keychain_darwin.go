//go:build darwin

package keychain

import (
	"os/exec"
)

func store(passphrase []byte) error {
	cmd := exec.Command("security", "add-generic-password",
		"-a", ServiceName,
		"-s", ServiceName,
		"-U",
		"-w", string(passphrase),
	)
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
	return out, nil
}

func forget() error {
	return exec.Command("security", "delete-generic-password",
		"-a", ServiceName,
		"-s", ServiceName,
	).Run()
}

func available() bool {
	_, err := exec.LookPath("security")
	return err == nil
}
