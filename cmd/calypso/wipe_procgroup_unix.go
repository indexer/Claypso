//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the child in its own process group so a Ctrl+C in
// the parent's terminal doesn't also kill calypso's clean-up step (the
// post-command wipe of the .env file). Only the wipeAndRun path uses this.
func setProcessGroup(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
