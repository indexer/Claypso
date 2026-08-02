// Agent-safety integration tests: the reveal gate (TTY / CALYPSO_UNATTENDED),
// vault lockdown, and output scrubbing in `pull -- cmd` runs. All commands
// here run headless (no TTY), which is exactly the situation of an AI coding
// agent driving a terminal.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupAgentVault creates a vault with one project and a known secret,
// returning (bin, vaultPath, envPath).
func setupAgentVault(t *testing.T) (string, string, string) {
	t.Helper()
	bin := calypsoPath(t)
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.enc")
	envPath := filepath.Join(dir, ".env")

	pw := "agent-gate-pass"
	if _, stderr, err := runCalypso(t, bin, pw, "--vault", vaultPath, "add", "myapp", "--path", envPath); err != nil {
		t.Fatalf("add: %v\nstderr: %s", err, stderr)
	}
	if _, stderr, err := runCalypso(t, bin, pw, "--vault", vaultPath, "set", "myapp",
		"API_KEY=supersecretvalue", "DEBUG=1"); err != nil {
		t.Fatalf("set: %v\nstderr: %s", err, stderr)
	}
	return bin, vaultPath, envPath
}

const agentPW = "agent-gate-pass"

// passphraseOnly runs the binary headless with the passphrase but WITHOUT
// CALYPSO_UNATTENDED — the situation of an agent that can unlock the vault.
func passphraseOnly(t *testing.T, bin string, args ...string) (string, string, error) {
	t.Helper()
	return runCalypsoEnv(t, bin, []string{"CALYPSO_PASSPHRASE=" + agentPW}, args...)
}

func TestIntegration_RevealGateHeadless(t *testing.T) {
	bin, vaultPath, _ := setupAgentVault(t)

	// Reveal-class ops must fail without a TTY and without CALYPSO_UNATTENDED.
	gated := [][]string{
		{"get", "myapp", "API_KEY", "--reveal"},
		{"pull", "myapp"},
		{"export", "--base64"},
	}
	for _, args := range gated {
		full := append([]string{"--vault", vaultPath}, args...)
		if _, stderr, err := passphraseOnly(t, bin, full...); err == nil {
			t.Errorf("%v should be denied headless", args)
		} else if !strings.Contains(stderr, "interactive terminal") {
			t.Errorf("%v: expected reveal-gate error, got: %s", args, stderr)
		}
	}

	// Masked ops keep working for the agent.
	for _, args := range [][]string{
		{"list"},
		{"get", "myapp", "API_KEY"},
		{"pull", "myapp", "--safe"},
	} {
		full := append([]string{"--vault", vaultPath}, args...)
		if _, stderr, err := passphraseOnly(t, bin, full...); err != nil {
			t.Errorf("%v should work headless: %v\nstderr: %s", args, err, stderr)
		}
	}

	// The gate opens with CALYPSO_UNATTENDED=1 (configured CI).
	stdout, stderr, err := runCalypso(t, bin, agentPW, "--vault", vaultPath, "get", "myapp", "API_KEY", "--reveal")
	if err != nil {
		t.Fatalf("get --reveal with CALYPSO_UNATTENDED: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "supersecretvalue") {
		t.Errorf("expected real value, got: %s", stdout)
	}
}

func TestIntegration_Lockdown(t *testing.T) {
	bin, vaultPath, _ := setupAgentVault(t)

	// Turning lockdown ON works headless (safe direction).
	if _, stderr, err := passphraseOnly(t, bin, "--vault", vaultPath, "lockdown", "on"); err != nil {
		t.Fatalf("lockdown on: %v\nstderr: %s", err, stderr)
	}
	stdout, _, err := passphraseOnly(t, bin, "--vault", vaultPath, "lockdown", "status")
	if err != nil || !strings.Contains(stdout, "on") {
		t.Fatalf("lockdown status: %v, stdout: %s", err, stdout)
	}

	// Lockdown blocks reveal even with CALYPSO_UNATTENDED=1.
	if _, stderr, err := runCalypso(t, bin, agentPW, "--vault", vaultPath, "get", "myapp", "API_KEY", "--reveal"); err == nil {
		t.Error("get --reveal should be blocked under lockdown")
	} else if !strings.Contains(stderr, "lockdown") {
		t.Errorf("expected lockdown error, got: %s", stderr)
	}
	if _, stderr, err := runCalypso(t, bin, agentPW, "--vault", vaultPath, "pull", "myapp"); err == nil {
		t.Error("plain pull should be blocked under lockdown")
	} else if !strings.Contains(stderr, "lockdown") {
		t.Errorf("expected lockdown error, got: %s", stderr)
	}
	if _, stderr, err := runCalypso(t, bin, agentPW, "--vault", vaultPath, "vault", "export-project", "myapp", filepath.Join(t.TempDir(), "x.cbk")); err == nil {
		t.Error("export-project should be blocked under lockdown")
	} else if !strings.Contains(stderr, "lockdown") {
		t.Errorf("expected lockdown error, got: %s", stderr)
	}

	// Masked and wipe workflows keep working under lockdown.
	if _, stderr, err := passphraseOnly(t, bin, "--vault", vaultPath, "pull", "myapp", "--safe"); err != nil {
		t.Errorf("pull --safe under lockdown: %v\nstderr: %s", err, stderr)
	}

	// lockdown off requires a TTY — headless must fail, keychain/env ignored.
	if _, stderr, err := passphraseOnly(t, bin, "--vault", vaultPath, "lockdown", "off"); err == nil {
		t.Error("lockdown off should require an interactive terminal")
	} else if !strings.Contains(stderr, "interactive terminal") {
		t.Errorf("expected TTY error, got: %s", stderr)
	}

	// vault downgrade is refused while lockdown is on.
	if _, stderr, err := passphraseOnly(t, bin, "--vault", vaultPath, "vault", "downgrade"); err == nil {
		t.Error("vault downgrade should fail under lockdown")
	} else if !strings.Contains(stderr, "lockdown") {
		t.Errorf("expected lockdown blocker, got: %s", stderr)
	}
}

func TestIntegration_WipeRunScrubsOutput(t *testing.T) {
	bin, vaultPath, envPath := setupAgentVault(t)

	// The child prints the real .env; the value must reach the terminal
	// concealed. Works under lockdown too — this is the sanctioned path.
	if _, stderr, err := passphraseOnly(t, bin, "--vault", vaultPath, "lockdown", "on"); err != nil {
		t.Fatalf("lockdown on: %v\nstderr: %s", err, stderr)
	}
	stdout, stderr, err := passphraseOnly(t, bin, "--vault", vaultPath, "pull", "myapp", "--", "cat", envPath)
	if err != nil {
		t.Fatalf("pull -- cat: %v\nstderr: %s", err, stderr)
	}
	if strings.Contains(stdout, "supersecretvalue") {
		t.Errorf("real value leaked to stdout: %s", stdout)
	}
	if !strings.Contains(stdout, "<concealed>") {
		t.Errorf("expected a name-free concealment marker in output, got: %s", stdout)
	}

	// After the run, the on-disk .env is wiped back to placeholders.
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env after wipe: %v", err)
	}
	if strings.Contains(string(data), "supersecretvalue") {
		t.Errorf(".env still holds the real value after wipe: %s", data)
	}

	// --no-scrub is unsafe and cannot be used headlessly or under lockdown.
	if _, stderr, err = passphraseOnly(t, bin, "--vault", vaultPath, "pull", "myapp", "--no-scrub", "--", "cat", envPath); err == nil {
		t.Error("pull --no-scrub should be blocked under lockdown")
	} else if !strings.Contains(stderr, "lockdown") {
		t.Errorf("expected lockdown denial, got: %s", stderr)
	}
}

func TestIntegration_InjectMode(t *testing.T) {
	bin, vaultPath, envPath := setupAgentVault(t)

	// Seed the .env with a marker so we can prove --inject never touches it.
	if err := os.WriteFile(envPath, []byte("# untouched marker\n"), 0o600); err != nil {
		t.Fatalf("seed env: %v", err)
	}

	// The child sees the value in its environment; the terminal sees it concealed.
	stdout, stderr, err := passphraseOnly(t, bin, "--vault", vaultPath,
		"pull", "myapp", "--inject", "--", "sh", "-c", "echo value=$API_KEY")
	if err != nil {
		t.Fatalf("pull --inject: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "value=<concealed>") {
		t.Errorf("expected concealed injected value, got: %s", stdout)
	}

	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	if string(data) != "# untouched marker\n" {
		t.Errorf("--inject modified the .env: %q", data)
	}

	// --inject without a trailing command is an error.
	if _, stderr, err := passphraseOnly(t, bin, "--vault", vaultPath, "pull", "myapp", "--inject"); err == nil {
		t.Error("--inject without command should fail")
	} else if !strings.Contains(stderr, "trailing command") {
		t.Errorf("expected trailing-command error, got: %s", stderr)
	}
}
