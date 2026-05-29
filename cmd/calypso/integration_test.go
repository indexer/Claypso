// Package main integration tests live across several files:
//   - integration_test.go      shared helpers + lifecycle + completion
//   - integration_multi_test.go  multi-project + unset/remove + wrong passphrase
//   - integration_pull_test.go   pull modes + export/import
//   - integration_env_test.go    multi-env + vault downgrade
package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// calypsoPath builds a fresh calypso binary into a tempdir and returns its path.
func calypsoPath(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "calypso")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build calypso: %v\n%s", err, out)
	}
	return bin
}

// runCalypso runs the binary with the given args, piping the passphrase via
// the canonical CALYPSO_PASSPHRASE env var. Returns stdout, stderr, and the
// run error.
func runCalypso(t *testing.T, bin string, passphrase string, args ...string) (string, string, error) {
	t.Helper()
	return runCalypsoEnv(t, bin, []string{"CALYPSO_PASSPHRASE=" + passphrase}, args...)
}

// runCalypsoEnv runs the binary with explicit extra env entries. Any passphrase
// var inherited from the test runner is stripped first, so the caller has full
// control over which passphrase var (if any) the binary sees — keeping these
// tests hermetic and letting us exercise env-var precedence.
func runCalypsoEnv(t *testing.T, bin string, extraEnv []string, args ...string) (string, string, error) {
	t.Helper()
	base := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "CALYPSO_PASSPHRASE=") || strings.HasPrefix(kv, "ENVHUB_PASSPHRASE=") {
			continue
		}
		base = append(base, kv)
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = append(base, extraEnv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func TestIntegration_FullLifecycle(t *testing.T) {
	bin := calypsoPath(t)
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.enc")
	envPath := filepath.Join(dir, "myapp", ".env")

	os.MkdirAll(filepath.Dir(envPath), 0o700)
	os.WriteFile(envPath, []byte("DB_HOST=localhost\nDB_PORT=5432\n"), 0o600)

	pw := "integration-test"

	// 1. Add project (auto-inits vault)
	stdout, stderr, err := runCalypso(t, bin, pw, "--vault", vaultPath, "add", "myapp", "--path", envPath)
	if err != nil {
		t.Fatalf("add: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stderr, "Vault created") && !strings.Contains(stdout, "Vault created") {
		t.Error("expected auto-init message on first use")
	}
	if !strings.Contains(stdout, "Registered") {
		t.Errorf("expected 'Registered', got: %s", stdout)
	}

	// 2. Push (import existing .env)
	stdout, stderr, err = runCalypso(t, bin, pw, "--vault", vaultPath, "push", "myapp", "--force")
	if err != nil {
		t.Fatalf("push: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "Imported 2 variable") {
		t.Errorf("expected 'Imported 2 variable', got: %s", stdout)
	}

	// 3. Set additional variables
	stdout, stderr, err = runCalypso(t, bin, pw, "--vault", vaultPath, "set", "myapp", "API_KEY=sk-test", "SECRET=abc123")
	if err != nil {
		t.Fatalf("set: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "Updated 2 variable") {
		t.Errorf("expected 'Updated 2 variable', got: %s", stdout)
	}

	// 4. Get (masked — should not show real values)
	stdout, stderr, err = runCalypso(t, bin, pw, "--vault", vaultPath, "get", "myapp", "API_KEY")
	if err != nil {
		t.Fatalf("get: %v\nstderr: %s", err, stderr)
	}
	if strings.Contains(stdout, "sk-test") {
		t.Error("masked output should not contain real API_KEY value")
	}

	// 5. Get --reveal (should show real values)
	stdout, stderr, err = runCalypso(t, bin, pw, "--vault", vaultPath, "get", "myapp", "API_KEY", "--reveal")
	if err != nil {
		t.Fatalf("get --reveal: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "sk-test") {
		t.Errorf("revealed output should contain real value, got: %s", stdout)
	}

	// 6. Pull (write to .env)
	_, stderr, err = runCalypso(t, bin, pw, "--vault", vaultPath, "pull", "myapp")
	if err != nil {
		t.Fatalf("pull: %v\nstderr: %s", err, stderr)
	}
	data, _ := os.ReadFile(envPath)
	content := string(data)
	if !strings.Contains(content, "DB_HOST=localhost") {
		t.Error("pulled .env should contain DB_HOST")
	}
	if !strings.Contains(content, "API_KEY=sk-test") {
		t.Error("pulled .env should contain API_KEY")
	}
	if !strings.Contains(content, "SECRET=abc123") {
		t.Error("pulled .env should contain SECRET")
	}

	// 7. Pull --safe (write masked values)
	if _, _, err = runCalypso(t, bin, pw, "--vault", vaultPath, "pull", "myapp", "--safe"); err != nil {
		t.Fatalf("pull --safe: %v", err)
	}
	data, _ = os.ReadFile(envPath)
	content = string(data)
	if !strings.Contains(content, "DB_HOST=****") {
		t.Error("safe .env should mask DB_HOST")
	}
	if strings.Contains(content, "localhost") || strings.Contains(content, "sk-test") {
		t.Error("safe .env must not contain real values")
	}

	// 8. List
	stdout, _, _ = runCalypso(t, bin, pw, "--vault", vaultPath, "list")
	if !strings.Contains(stdout, "myapp") || !strings.Contains(stdout, "4") {
		t.Errorf("list should show myapp with 4 vars, got: %s", stdout)
	}
}

func TestIntegration_Completion(t *testing.T) {
	bin := calypsoPath(t)
	for _, shell := range []string{"bash", "zsh", "fish"} {
		stdout, _, err := runCalypso(t, bin, "", "completion", shell)
		if err != nil {
			t.Errorf("completion %s: %v", shell, err)
			continue
		}
		if len(stdout) < 20 {
			t.Errorf("completion %s: output too short (%d bytes)", shell, len(stdout))
		}
	}
}

// TestIntegration_PassphraseEnvVars exercises the canonical vs. legacy
// passphrase env vars: the canonical CALYPSO_PASSPHRASE works without a
// warning, the legacy ENVHUB_PASSPHRASE still works but warns, and the
// canonical var wins when both are set.
func TestIntegration_PassphraseEnvVars(t *testing.T) {
	bin := calypsoPath(t)
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.enc")
	const pw = "passphrase-precedence"

	if _, stderr, err := runCalypsoEnv(t, bin,
		[]string{"CALYPSO_PASSPHRASE=" + pw}, "--vault", vaultPath, "init"); err != nil {
		t.Fatalf("init: %v\nstderr: %s", err, stderr)
	}

	// Canonical var: works, no deprecation warning.
	if _, stderr, err := runCalypsoEnv(t, bin,
		[]string{"CALYPSO_PASSPHRASE=" + pw}, "--vault", vaultPath, "list"); err != nil {
		t.Fatalf("list with CALYPSO_PASSPHRASE: %v\nstderr: %s", err, stderr)
	} else if strings.Contains(stderr, "deprecated") {
		t.Errorf("canonical var should not warn; stderr: %s", stderr)
	}

	// Legacy var: still works, but warns.
	if _, stderr, err := runCalypsoEnv(t, bin,
		[]string{"ENVHUB_PASSPHRASE=" + pw}, "--vault", vaultPath, "list"); err != nil {
		t.Fatalf("list with ENVHUB_PASSPHRASE: %v\nstderr: %s", err, stderr)
	} else if !strings.Contains(stderr, "deprecated") {
		t.Errorf("legacy var should emit a deprecation warning; stderr: %s", stderr)
	}

	// Both set: canonical wins. ENVHUB holds a wrong value, so a successful run
	// proves CALYPSO_PASSPHRASE took precedence (otherwise decrypt would fail).
	if _, stderr, err := runCalypsoEnv(t, bin,
		[]string{"CALYPSO_PASSPHRASE=" + pw, "ENVHUB_PASSPHRASE=wrong-passphrase"},
		"--vault", vaultPath, "list"); err != nil {
		t.Fatalf("canonical should win over legacy: %v\nstderr: %s", err, stderr)
	} else if strings.Contains(stderr, "deprecated") {
		t.Errorf("no deprecation warning expected when canonical is used; stderr: %s", stderr)
	}
}
