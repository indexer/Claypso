package main

import (
	"os"
	"strings"
	"testing"
)

func TestIntegration_VaultRekey(t *testing.T) {
	bin, vaultPath, _ := setupAgentVault(t)
	newPW := "rotated-passphrase-9"

	// Headless rekey without CALYPSO_NEW_PASSPHRASE: refused.
	if _, stderr, err := runCalypso(t, bin, agentPW, "--vault", vaultPath, "vault", "rekey"); err == nil {
		t.Error("rekey without new passphrase source should fail headless")
	} else if !strings.Contains(stderr, "CALYPSO_NEW_PASSPHRASE") {
		t.Errorf("expected pointer to CALYPSO_NEW_PASSPHRASE, got: %s", stderr)
	}

	// Headless rekey without CALYPSO_UNATTENDED: reveal-gate refusal.
	if _, stderr, err := runCalypsoEnv(t, bin,
		[]string{"CALYPSO_PASSPHRASE=" + agentPW, "CALYPSO_NEW_PASSPHRASE=" + newPW},
		"--vault", vaultPath, "vault", "rekey"); err == nil {
		t.Error("rekey should need CALYPSO_UNATTENDED=1 headless")
	} else if !strings.Contains(stderr, "interactive terminal") {
		t.Errorf("expected reveal-gate error, got: %s", stderr)
	}

	// CI rotation: both env vars present.
	stdout, stderr, err := runCalypsoEnv(t, bin,
		[]string{"CALYPSO_PASSPHRASE=" + agentPW, "CALYPSO_UNATTENDED=1", "CALYPSO_NEW_PASSPHRASE=" + newPW},
		"--vault", vaultPath, "vault", "rekey")
	if err != nil {
		t.Fatalf("rekey: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "re-encrypted") {
		t.Errorf("unexpected rekey output: %s", stdout)
	}

	// Old passphrase dead, new one works.
	if _, stderr, _ := runCalypso(t, bin, agentPW, "--vault", vaultPath, "list"); !strings.Contains(stderr, "wrong passphrase") {
		t.Errorf("old passphrase should be rejected, stderr: %s", stderr)
	}
	if _, stderr, err := runCalypso(t, bin, newPW, "--vault", vaultPath, "get", "myapp", "API_KEY", "--reveal"); err != nil {
		t.Fatalf("new passphrase should work: %v\nstderr: %s", err, stderr)
	}

	// Pre-rekey backup still opens with the OLD passphrase.
	bak := vaultPath + ".pre-rekey.bak"
	if _, stderr, err := runCalypso(t, bin, agentPW, "--vault", bak, "list"); err != nil {
		t.Errorf("pre-rekey backup should decrypt with old passphrase: %v\nstderr: %s", err, stderr)
	}

	// New passphrase identical to current: refused.
	if _, stderr, err := runCalypsoEnv(t, bin,
		[]string{"CALYPSO_PASSPHRASE=" + newPW, "CALYPSO_UNATTENDED=1", "CALYPSO_NEW_PASSPHRASE=" + newPW},
		"--vault", vaultPath, "vault", "rekey"); err == nil {
		t.Error("rekey to the same passphrase should fail")
	} else if !strings.Contains(stderr, "identical") {
		t.Errorf("expected identical-passphrase error, got: %s", stderr)
	}
}

func TestIntegration_RekeyBlockedUnderLockdown(t *testing.T) {
	bin, vaultPath, _ := setupAgentVault(t)

	if _, stderr, err := passphraseOnly(t, bin, "--vault", vaultPath, "lockdown", "on"); err != nil {
		t.Fatalf("lockdown on: %v\nstderr: %s", err, stderr)
	}
	if _, stderr, err := runCalypsoEnv(t, bin,
		[]string{"CALYPSO_PASSPHRASE=" + agentPW, "CALYPSO_UNATTENDED=1", "CALYPSO_NEW_PASSPHRASE=stolen-vault-pw1"},
		"--vault", vaultPath, "vault", "rekey"); err == nil {
		t.Error("rekey must be blocked under lockdown — even with CALYPSO_UNATTENDED")
	} else if !strings.Contains(stderr, "lockdown") {
		t.Errorf("expected lockdown error, got: %s", stderr)
	}
}

func TestIntegration_LockdownAllowUnattended(t *testing.T) {
	bin, vaultPath, envPath := setupAgentVault(t)

	if _, stderr, err := passphraseOnly(t, bin, "--vault", vaultPath, "lockdown", "on", "--allow-unattended"); err != nil {
		t.Fatalf("lockdown on --allow-unattended: %v\nstderr: %s", err, stderr)
	}
	stdout, _, _ := passphraseOnly(t, bin, "--vault", vaultPath, "lockdown", "status")
	if !strings.Contains(stdout, "unattended pipelines exempt") {
		t.Errorf("status should show the exemption, got: %s", stdout)
	}

	// Agent without CALYPSO_UNATTENDED: still blocked.
	if _, stderr, err := passphraseOnly(t, bin, "--vault", vaultPath, "get", "myapp", "API_KEY", "--reveal"); err == nil {
		t.Error("reveal without CALYPSO_UNATTENDED should stay blocked")
	} else if !strings.Contains(stderr, "lockdown") {
		t.Errorf("expected lockdown error, got: %s", stderr)
	}

	// Deploy pipeline with CALYPSO_UNATTENDED=1: plain pull works.
	if _, stderr, err := runCalypso(t, bin, agentPW, "--vault", vaultPath, "pull", "myapp", "--force"); err != nil {
		t.Fatalf("CI pull under soft lockdown should work: %v\nstderr: %s", err, stderr)
	}
	data := readFileOrFail(t, envPath)
	if !strings.Contains(data, "supersecretvalue") {
		t.Errorf("CI pull should write real values, got: %s", data)
	}

	// Hard lockdown re-applied clears the exemption.
	if _, stderr, err := passphraseOnly(t, bin, "--vault", vaultPath, "lockdown", "on"); err != nil {
		t.Fatalf("re-tighten lockdown: %v\nstderr: %s", err, stderr)
	}
	if _, _, err := runCalypso(t, bin, agentPW, "--vault", vaultPath, "pull", "myapp", "--force"); err == nil {
		t.Error("hard lockdown should block CI pull again")
	}
}

func readFileOrFail(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
