package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegration_Exposure(t *testing.T) {
	bin, vaultPath, envPath := setupAgentVault(t)

	// Fresh vault, .env still holds whatever `add` found (nothing) — clean.
	stdout, stderr, err := passphraseOnly(t, bin, "--vault", vaultPath, "exposure")
	if err != nil {
		t.Fatalf("exposure on clean state: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "No exposed") {
		t.Errorf("expected clean report, got: %s", stdout)
	}

	// Plain pull leaves real values on disk → hot, and --max-age 0 fails.
	if _, stderr, err := runCalypso(t, bin, agentPW, "--vault", vaultPath, "pull", "myapp", "--force"); err != nil {
		t.Fatalf("pull: %v\nstderr: %s", err, stderr)
	}
	stdout, _, err = passphraseOnly(t, bin, "--vault", vaultPath, "exposure", "--max-age", "0")
	if err == nil {
		t.Error("exposure should fail while .env is hot")
	}
	if !strings.Contains(stdout, "API_KEY") {
		t.Errorf("hot key should be named, got: %s", stdout)
	}
	if strings.Contains(stdout, "supersecretvalue") {
		t.Errorf("exposure output leaked a real value: %s", stdout)
	}

	// Young exposure with a generous max-age: reported but exit 0.
	if _, stderr, err := passphraseOnly(t, bin, "--vault", vaultPath, "exposure", "--max-age", "1h"); err != nil {
		t.Errorf("young exposure should not fail with 1h max-age: %v\nstderr: %s", err, stderr)
	}

	// --fix rewrites the file with masked values and stops failing.
	if _, stderr, err := passphraseOnly(t, bin, "--vault", vaultPath, "exposure", "--max-age", "0", "--fix"); err != nil {
		t.Fatalf("exposure --fix: %v\nstderr: %s", err, stderr)
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("read env: %v", err)
	}
	if strings.Contains(string(data), "supersecretvalue") {
		t.Errorf("--fix left the real value on disk: %s", data)
	}
	if _, _, err := passphraseOnly(t, bin, "--vault", vaultPath, "exposure", "--max-age", "0"); err != nil {
		t.Errorf("exposure should be clean after --fix: %v", err)
	}
}

func TestIntegration_ExposureFixGuardsUnsyncedEdits(t *testing.T) {
	bin, vaultPath, envPath := setupAgentVault(t)

	// Real values on disk plus a hand-added key the vault doesn't know.
	if _, stderr, err := runCalypso(t, bin, agentPW, "--vault", vaultPath, "pull", "myapp", "--force"); err != nil {
		t.Fatalf("pull: %v\nstderr: %s", err, stderr)
	}
	f, err := os.OpenFile(envPath, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("HOTFIX=hand-edited-value\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	// --fix without --force must refuse and keep failing.
	stdout, _, err := passphraseOnly(t, bin, "--vault", vaultPath, "exposure", "--max-age", "0", "--fix")
	if err == nil {
		t.Error("exposure --fix should still fail when the fix was refused")
	}
	if !strings.Contains(stdout, "skipped --fix") {
		t.Errorf("expected unsynced-edit refusal, got: %s", stdout)
	}
	data, _ := os.ReadFile(envPath)
	if !strings.Contains(string(data), "hand-edited-value") {
		t.Error("--fix destroyed unsynced local edits despite refusing")
	}

	// --fix --force overwrites.
	if _, stderr, err := passphraseOnly(t, bin, "--vault", vaultPath, "exposure", "--max-age", "0", "--fix", "--force"); err != nil {
		t.Fatalf("exposure --fix --force: %v\nstderr: %s", err, stderr)
	}
	data, _ = os.ReadFile(envPath)
	if strings.Contains(string(data), "supersecretvalue") || strings.Contains(string(data), "hand-edited-value") {
		t.Errorf("--fix --force should mask everything: %s", data)
	}
}

func TestIntegration_AgentInit(t *testing.T) {
	bin := calypsoPath(t)
	dir := t.TempDir()

	// agent init needs no vault at all.
	for _, tool := range []string{"claude-code", "codex", "opencode"} {
		if stdout, stderr, err := runCalypsoEnv(t, bin, nil, "agent", "init", tool, "--dir", dir); err != nil {
			t.Fatalf("agent init %s: %v\nstderr: %s\nstdout: %s", tool, err, stderr, stdout)
		}
	}
	for _, f := range []string{
		filepath.Join(dir, ".claude", "settings.json"),
		filepath.Join(dir, "AGENTS.md"),
		filepath.Join(dir, "opencode.json"),
	} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("expected %s to exist: %v", f, err)
		}
	}

	// Dry-run leaves a fresh directory untouched.
	dir2 := t.TempDir()
	if _, stderr, err := runCalypsoEnv(t, bin, nil, "agent", "init", "claude-code", "--dir", dir2, "--dry-run"); err != nil {
		t.Fatalf("agent init --dry-run: %v\nstderr: %s", err, stderr)
	}
	if _, err := os.Stat(filepath.Join(dir2, ".claude")); !os.IsNotExist(err) {
		t.Error("--dry-run created files")
	}
}
