//go:build windows

package main

import "os/exec"

// setProcessGroup is a no-op on Windows: there's no SysProcAttr.Setpgid
// equivalent in the standard library. Ctrl+C handling on Windows is
// done via console event groups, which Go's exec package wires up for
// us automatically — the wipeAndRun child shares the parent's console.
func setProcessGroup(c *exec.Cmd) {}
