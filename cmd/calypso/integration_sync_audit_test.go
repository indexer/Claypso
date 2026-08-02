package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubCLI drops an executable shell script named name into dir that appends
// its args and stdin to captureFile, then echoes the marker (so scrubbing
// can be asserted against a CLI that leaks values back to the terminal).
func stubCLI(t *testing.T, dir, name, captureFile string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"{ printf 'ARGS:%s\\n' \"$*\"; cat; printf '\\n'; } >> \"" + captureFile + "\"\n" +
		"echo \"set API_KEY=supersecretvalue\"\n" // deliberate leak to test scrubbing
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil { //nolint:gosec // test stub must be executable
		t.Fatalf("write stub %s: %v", name, err)
	}
}

// syncEnv builds the env for a sync run: passphrase + CALYPSO_UNATTENDED +
// PATH with the stub dir first (os/exec dedupes; last PATH entry wins).
func syncEnv(stubDir string) []string {
	return []string{
		"CALYPSO_PASSPHRASE=" + agentPW,
		"CALYPSO_UNATTENDED=1",
		"PATH=" + stubDir + string(os.PathListSeparator) + os.Getenv("PATH"),
	}
}

func TestIntegration_SyncFly(t *testing.T) {
	bin, vaultPath, _ := setupAgentVault(t)
	stubDir := t.TempDir()
	capture := filepath.Join(stubDir, "captured.txt")
	stubCLI(t, stubDir, "flyctl", capture)

	stdout, stderr, err := runCalypsoEnv(t, bin, syncEnv(stubDir),
		"--vault", vaultPath, "sync", "fly", "myapp", "--app", "myapp-prod")
	if err != nil {
		t.Fatalf("sync fly: %v\nstderr: %s", err, stderr)
	}

	got := readFileOrFail(t, capture)
	if !strings.Contains(got, "ARGS:secrets import -a myapp-prod") {
		t.Errorf("flyctl called with wrong args: %s", got)
	}
	if !strings.Contains(got, "API_KEY=supersecretvalue") {
		t.Errorf("real value should reach flyctl stdin: %s", got)
	}
	// The stub echoed the value back — calypso's terminal output must conceal it.
	if strings.Contains(stdout, "supersecretvalue") {
		t.Errorf("sync leaked value to terminal: %s", stdout)
	}
	if !strings.Contains(stdout, "<concealed>") {
		t.Errorf("expected scrubbed echo, got: %s", stdout)
	}
}

func TestIntegration_SyncK8s(t *testing.T) {
	bin, vaultPath, _ := setupAgentVault(t)
	stubDir := t.TempDir()
	capture := filepath.Join(stubDir, "captured.txt")
	stubCLI(t, stubDir, "kubectl", capture)

	_, stderr, err := runCalypsoEnv(t, bin, syncEnv(stubDir),
		"--vault", vaultPath, "sync", "k8s", "myapp", "--namespace", "prod")
	if err != nil {
		t.Fatalf("sync k8s: %v\nstderr: %s", err, stderr)
	}
	got := readFileOrFail(t, capture)
	if !strings.Contains(got, "ARGS:apply -f -") {
		t.Errorf("kubectl called with wrong args: %s", got)
	}
	if !strings.Contains(got, "kind: Secret") || !strings.Contains(got, "namespace: prod") {
		t.Errorf("manifest malformed: %s", got)
	}
	if !strings.Contains(got, "API_KEY: c3VwZXJzZWNyZXR2YWx1ZQ==") {
		t.Errorf("manifest missing base64 value: %s", got)
	}
	if strings.Contains(got, "API_KEY=supersecretvalue") {
		t.Errorf("manifest should carry base64, not plaintext lines: %s", got)
	}
}

func TestIntegration_SyncGated(t *testing.T) {
	bin, vaultPath, _ := setupAgentVault(t)

	// Headless without CALYPSO_UNATTENDED: reveal gate.
	if _, stderr, err := passphraseOnly(t, bin, "--vault", vaultPath, "sync", "fly", "myapp"); err == nil {
		t.Error("sync should be reveal-gated headless")
	} else if !strings.Contains(stderr, "interactive terminal") {
		t.Errorf("expected reveal-gate error, got: %s", stderr)
	}

	// Hard lockdown blocks sync even for CI.
	if _, stderr, err := passphraseOnly(t, bin, "--vault", vaultPath, "lockdown", "on"); err != nil {
		t.Fatalf("lockdown on: %v\nstderr: %s", err, stderr)
	}
	if _, stderr, err := runCalypso(t, bin, agentPW, "--vault", vaultPath, "sync", "fly", "myapp"); err == nil {
		t.Error("sync should be blocked under hard lockdown")
	} else if !strings.Contains(stderr, "lockdown") {
		t.Errorf("expected lockdown error, got: %s", stderr)
	}
}

func TestIntegration_SyncDryRunMasks(t *testing.T) {
	bin, vaultPath, _ := setupAgentVault(t)
	stdout, stderr, err := runCalypso(t, bin, agentPW, "--vault", vaultPath, "sync", "fly", "myapp", "--dry-run")
	if err != nil {
		t.Fatalf("sync fly --dry-run: %v\nstderr: %s", err, stderr)
	}
	if strings.Contains(stdout, "supersecretvalue") {
		t.Errorf("dry-run leaked a real value: %s", stdout)
	}
	if !strings.Contains(stdout, "API_KEY="+maskFixed) {
		t.Errorf("dry-run should list masked keys, got: %s", stdout)
	}
}

func TestIntegration_AuditTrail(t *testing.T) {
	bin, vaultPath, _ := setupAgentVault(t)
	auditPath := vaultPath + ".audit.log"

	// Denied attempt (headless reveal) must land in the log.
	if _, _, err := passphraseOnly(t, bin, "--vault", vaultPath, "get", "myapp", "API_KEY", "--reveal"); err == nil {
		t.Fatal("reveal should be denied headless")
	}
	// Permitted plain pull too.
	if _, stderr, err := runCalypso(t, bin, agentPW, "--vault", vaultPath, "pull", "myapp", "--force"); err != nil {
		t.Fatalf("pull: %v\nstderr: %s", err, stderr)
	}

	stdout, stderr, err := passphraseOnly(t, bin, "--vault", vaultPath, "audit", "list")
	if err != nil {
		t.Fatalf("audit list: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "get-reveal") || !strings.Contains(stdout, "denied") {
		t.Errorf("denied reveal missing from audit list: %s", stdout)
	}
	if !strings.Contains(stdout, "pull") || !strings.Contains(stdout, "ok") {
		t.Errorf("permitted pull missing from audit list: %s", stdout)
	}
	if strings.Contains(stdout, "supersecretvalue") {
		t.Errorf("audit list leaked a value: %s", stdout)
	}

	// Chain verifies clean.
	stdout, stderr, err = passphraseOnly(t, bin, "--vault", vaultPath, "audit", "verify")
	if err != nil {
		t.Fatalf("audit verify: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "OK") {
		t.Errorf("expected chain OK, got: %s", stdout)
	}

	// Tamper → verify fails.
	data := readFileOrFail(t, auditPath)
	tampered := strings.Replace(data, `"outcome":"denied"`, `"outcome":"ok"`, 1)
	if tampered == data {
		t.Fatal("tamper substitution failed")
	}
	if err := os.WriteFile(auditPath, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, stderr, err := passphraseOnly(t, bin, "--vault", vaultPath, "audit", "verify"); err == nil {
		t.Error("audit verify should fail after tampering")
	} else if !strings.Contains(stderr, "INVALID") {
		t.Errorf("expected INVALID chain error, got: %s", stderr)
	}

	// CALYPSO_AUDIT=0 disables logging.
	before := len(strings.Split(strings.TrimSpace(readFileOrFail(t, auditPath)), "\n"))
	if _, _, err := runCalypsoEnv(t, bin,
		[]string{"CALYPSO_PASSPHRASE=" + agentPW, "CALYPSO_UNATTENDED=1", "CALYPSO_AUDIT=0"},
		"--vault", vaultPath, "pull", "myapp", "--force"); err != nil {
		t.Fatalf("pull with audit disabled: %v", err)
	}
	after := len(strings.Split(strings.TrimSpace(readFileOrFail(t, auditPath)), "\n"))
	if after != before {
		t.Errorf("CALYPSO_AUDIT=0 should suppress logging: %d -> %d lines", before, after)
	}
}
