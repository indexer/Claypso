package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegration_MultiProject(t *testing.T) {
	bin := calypsoPath(t)
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.enc")
	pw := "multi-test"

	envA := filepath.Join(dir, "alpha", ".env")
	envB := filepath.Join(dir, "beta", ".env")
	os.MkdirAll(filepath.Dir(envA), 0o700)
	os.MkdirAll(filepath.Dir(envB), 0o700)
	os.WriteFile(envA, []byte("DB_HOST=localhost\nAPI_KEY=sk-alpha\n"), 0o600)
	os.WriteFile(envB, []byte("DB_HOST=localhost\nAPI_KEY=sk-beta\nBETA_ONLY=xyz\n"), 0o600)

	runCalypso(t, bin, pw, "--vault", vaultPath, "add", "alpha", "--path", envA)
	runCalypso(t, bin, pw, "--vault", vaultPath, "push", "alpha", "--force")
	runCalypso(t, bin, pw, "--vault", vaultPath, "add", "beta", "--path", envB)
	runCalypso(t, bin, pw, "--vault", vaultPath, "push", "beta", "--force")

	stdout, _, err := runCalypso(t, bin, pw, "--vault", vaultPath, "diff", "alpha", "beta", "--reveal")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !strings.Contains(stdout, "API_KEY") || !strings.Contains(stdout, "differs") {
		t.Errorf("diff should show API_KEY differs: %s", stdout)
	}
	if !strings.Contains(stdout, "BETA_ONLY") || !strings.Contains(stdout, "only in beta") {
		t.Errorf("diff should show BETA_ONLY only in beta: %s", stdout)
	}

	stdout, _, err = runCalypso(t, bin, pw, "--vault", vaultPath, "gaps")
	if err != nil {
		t.Fatalf("gaps: %v", err)
	}
	if !strings.Contains(stdout, "No gaps") {
		t.Errorf("expected 'No gaps', got: %s", stdout)
	}
}

func TestIntegration_UnsetAndRemove(t *testing.T) {
	bin := calypsoPath(t)
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.enc")
	envPath := filepath.Join(dir, "svc", ".env")
	os.MkdirAll(filepath.Dir(envPath), 0o700)
	os.WriteFile(envPath, []byte("A=1\nB=2\nC=3\n"), 0o600)

	pw := "unset-test"
	runCalypso(t, bin, pw, "--vault", vaultPath, "add", "svc", "--path", envPath)
	runCalypso(t, bin, pw, "--vault", vaultPath, "push", "svc", "--force")

	stdout, _, err := runCalypso(t, bin, pw, "--vault", vaultPath, "unset", "svc", "B")
	if err != nil {
		t.Fatalf("unset: %v", err)
	}
	if !strings.Contains(stdout, "Removed 1") {
		t.Errorf("expected 'Removed 1', got: %s", stdout)
	}

	stdout, _, _ = runCalypso(t, bin, pw, "--vault", vaultPath, "get", "svc", "--reveal")
	if strings.Contains(stdout, "B=2") {
		t.Error("B should be removed after unset")
	}
	if !strings.Contains(stdout, "A=1") || !strings.Contains(stdout, "C=3") {
		t.Error("A and C should still exist after unset")
	}

	stdout, _, err = runCalypso(t, bin, pw, "--vault", vaultPath, "remove", "svc")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !strings.Contains(stdout, "Removed") {
		t.Errorf("expected 'Removed', got: %s", stdout)
	}

	stdout, _, _ = runCalypso(t, bin, pw, "--vault", vaultPath, "list")
	if strings.Contains(stdout, "svc") {
		t.Error("svc should not appear after remove")
	}
}

func TestIntegration_WrongPassphrase(t *testing.T) {
	bin := calypsoPath(t)
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.enc")

	runCalypso(t, bin, "correct!", "--vault", vaultPath, "add", "x", "--path", filepath.Join(dir, ".env"))

	cmd := exec.Command(bin, "--vault", vaultPath, "get", "x")
	cmd.Env = append(os.Environ(), "ENVHUB_PASSPHRASE=wrong")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err == nil {
		t.Fatal("expected failure with wrong passphrase")
	}
	if !strings.Contains(stderr.String(), "wrong passphrase") {
		t.Errorf("expected wrong passphrase error, got: %s", stderr.String())
	}
}
