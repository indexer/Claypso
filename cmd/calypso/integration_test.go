package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// calypsoPath resolves the binary under test. Prefers the one in the current
// directory; falls back to a `go run`-style invocation if needed.
func calypsoPath(t *testing.T) string {
	t.Helper()
	// Build a fresh binary for integration tests.
	bin := filepath.Join(t.TempDir(), "calypso")
	cmd := exec.Command("go", "build", "-o", bin, ".")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("build calypso: %v\n%s", err, out)
	}
	return bin
}

func runCalypso(t *testing.T, bin string, passphrase string, args ...string) (string, string, error) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "ENVHUB_PASSPHRASE="+passphrase)
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

	// Setup: create an initial .env file
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
	stdout, stderr, err = runCalypso(t, bin, pw, "--vault", vaultPath, "pull", "myapp")
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
	_, _, err = runCalypso(t, bin, pw, "--vault", vaultPath, "pull", "myapp", "--safe")
	if err != nil {
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
	if !strings.Contains(stdout, "myapp") || !strings.Contains(stdout, "4") { // 4 vars now
		t.Errorf("list should show myapp with 4 vars, got: %s", stdout)
	}
}

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

	// Add and push both projects
	runCalypso(t, bin, pw, "--vault", vaultPath, "add", "alpha", "--path", envA)
	runCalypso(t, bin, pw, "--vault", vaultPath, "push", "alpha", "--force")
	runCalypso(t, bin, pw, "--vault", vaultPath, "add", "beta", "--path", envB)
	runCalypso(t, bin, pw, "--vault", vaultPath, "push", "beta", "--force")

	// Diff
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

	// Gaps (API_KEY is shared in 2 projects, not a gap; BETA_ONLY is in 1, not flagged)
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

	// Unset one key
	stdout, _, err := runCalypso(t, bin, pw, "--vault", vaultPath, "unset", "svc", "B")
	if err != nil {
		t.Fatalf("unset: %v", err)
	}
	if !strings.Contains(stdout, "Removed 1") {
		t.Errorf("expected 'Removed 1', got: %s", stdout)
	}

	// Verify B is gone
	stdout, _, _ = runCalypso(t, bin, pw, "--vault", vaultPath, "get", "svc", "--reveal")
	if strings.Contains(stdout, "B=2") {
		t.Error("B should be removed after unset")
	}
	if !strings.Contains(stdout, "A=1") || !strings.Contains(stdout, "C=3") {
		t.Error("A and C should still exist after unset")
	}

	// Remove project
	stdout, _, err = runCalypso(t, bin, pw, "--vault", vaultPath, "remove", "svc")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !strings.Contains(stdout, "Removed") {
		t.Errorf("expected 'Removed', got: %s", stdout)
	}

	// Verify svc is gone from list
	stdout, _, _ = runCalypso(t, bin, pw, "--vault", vaultPath, "list")
	if strings.Contains(stdout, "svc") {
		t.Error("svc should not appear after remove")
	}
}

func TestIntegration_WrongPassphrase(t *testing.T) {
	bin := calypsoPath(t)
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.enc")

	// Init with correct password
	runCalypso(t, bin, "correct", "--vault", vaultPath, "add", "x", "--path", filepath.Join(dir, ".env"))

	// Try with wrong password via ENVHUB_PASSPHRASE
	cmd := exec.Command(bin, "--vault", vaultPath, "get", "x")
	cmd.Env = append(os.Environ(), "ENVHUB_PASSPHRASE=wrong")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("expected failure with wrong passphrase")
	}
	if !strings.Contains(stderr.String(), "wrong passphrase") {
		t.Errorf("expected wrong passphrase error, got: %s", stderr.String())
	}
}

func TestIntegration_PullModes(t *testing.T) {
	bin := calypsoPath(t)
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.enc")
	envPath := filepath.Join(dir, "svc", ".env")
	os.MkdirAll(filepath.Dir(envPath), 0o700)
	os.WriteFile(envPath, []byte("KEY=original\n"), 0o600)

	pw := "pull-modes"
	runCalypso(t, bin, pw, "--vault", vaultPath, "add", "svc", "--path", envPath)
	runCalypso(t, bin, pw, "--vault", vaultPath, "push", "svc", "--force")
	runCalypso(t, bin, pw, "--vault", vaultPath, "set", "svc", "SECRET=xyz")

	// --safe
	runCalypso(t, bin, pw, "--vault", vaultPath, "pull", "svc", "--safe")
	data, _ := os.ReadFile(envPath)
	if !strings.Contains(string(data), "****") {
		t.Error("--safe should write ****")
	}
	if strings.Contains(string(data), "original") || strings.Contains(string(data), "xyz") {
		t.Error("--safe must not leak values")
	}

	// --example
	runCalypso(t, bin, pw, "--vault", vaultPath, "pull", "svc", "--example")
	data, _ = os.ReadFile(envPath)
	content := string(data)
	if !strings.Contains(content, "KEY=") || !strings.Contains(content, "SECRET=") {
		t.Error("--example should have empty values with key names")
	}
	if strings.Contains(content, "xyz") || strings.Contains(content, "****") {
		t.Error("--example should not have values or placeholders")
	}

	// Normal pull restores real values
	runCalypso(t, bin, pw, "--vault", vaultPath, "pull", "svc")
	data, _ = os.ReadFile(envPath)
	if !strings.Contains(string(data), "SECRET=xyz") {
		t.Error("normal pull should restore real values")
	}
}

func TestIntegration_ExportImport(t *testing.T) {
	bin := calypsoPath(t)
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.enc")
	exportPath := filepath.Join(dir, "export.enc")
	envPath := filepath.Join(dir, "app", ".env")
	os.MkdirAll(filepath.Dir(envPath), 0o700)
	os.WriteFile(envPath, []byte("A=1\n"), 0o600)

	pw := "export-test"
	runCalypso(t, bin, pw, "--vault", vaultPath, "add", "app", "--path", envPath)
	runCalypso(t, bin, pw, "--vault", vaultPath, "push", "app", "--force")

	// Export
	stdout, _, err := runCalypso(t, bin, pw, "--vault", vaultPath, "export", exportPath)
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if !strings.Contains(stdout, "Exported") {
		t.Errorf("export output: %s", stdout)
	}

	// Base64 export
	stdout, _, err = runCalypso(t, bin, pw, "--vault", vaultPath, "export", "--base64")
	if err != nil {
		t.Fatalf("export --base64: %v", err)
	}
	if len(stdout) < 10 {
		t.Error("base64 export should produce output")
	}

	// Import into a fresh vault
	vault2 := filepath.Join(dir, "vault2.enc")
	cmd := exec.Command(bin, "--vault", vault2, "import", exportPath)
	cmd.Env = os.Environ()
	var impStderr bytes.Buffer
	cmd.Stderr = &impStderr
	cmd.Stdout = &impStderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("import: %v\n%s", err, impStderr.String())
	}

	// Verify imported vault works
	stdout, _, err = runCalypso(t, bin, pw, "--vault", vault2, "list")
	if err != nil {
		t.Fatalf("list imported: %v", err)
	}
	if !strings.Contains(stdout, "app") {
		t.Errorf("imported vault should contain 'app': %s", stdout)
	}
}

// TestIntegration_MultiEnv exercises the multi-env feature end-to-end:
// add a project with a `dev` env, attach `prod`, set values per env, pull
// to per-env paths, diff across envs, copy an env, and verify intra-project
// gap detection. ENVHUB_PASSPHRASE makes the run unattended (skips confirm).
func TestIntegration_MultiEnv(t *testing.T) {
	bin := calypsoPath(t)
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.enc")
	devEnv := filepath.Join(dir, ".env.dev")
	prodEnv := filepath.Join(dir, ".env.prod")
	stagingEnv := filepath.Join(dir, ".env.staging")
	pw := "multi-env-test"

	// 1. Create project with explicit dev env.
	if _, stderr, err := runCalypso(t, bin, pw, "--vault", vaultPath,
		"add", "myapp", "--path", devEnv, "--env", "dev"); err != nil {
		t.Fatalf("add dev: %v\nstderr: %s", err, stderr)
	}

	// 2. Add prod env via name@env syntax.
	if _, stderr, err := runCalypso(t, bin, pw, "--vault", vaultPath,
		"add", "myapp@prod", "--path", prodEnv); err != nil {
		t.Fatalf("add @prod: %v\nstderr: %s", err, stderr)
	}

	// 3. set/get against each env independently.
	runCalypso(t, bin, pw, "--vault", vaultPath, "set", "myapp@dev", "DB_HOST=localhost", "DEBUG=1")
	runCalypso(t, bin, pw, "--vault", vaultPath, "set", "myapp@prod", "DB_HOST=db.prod.internal", "CDN=https://cdn.example.com")

	stdout, _, _ := runCalypso(t, bin, pw, "--vault", vaultPath, "get", "myapp@dev", "DB_HOST", "--reveal")
	if !strings.Contains(stdout, "localhost") {
		t.Errorf("dev DB_HOST should be localhost, got: %s", stdout)
	}
	stdout, _, _ = runCalypso(t, bin, pw, "--vault", vaultPath, "get", "myapp@prod", "DB_HOST", "--reveal")
	if !strings.Contains(stdout, "db.prod.internal") {
		t.Errorf("prod DB_HOST should be db.prod.internal, got: %s", stdout)
	}

	// 4. Bare `myapp` (no @env) should fail now that there are two envs.
	if _, stderr, err := runCalypso(t, bin, pw, "--vault", vaultPath, "get", "myapp", "DB_HOST"); err == nil {
		t.Errorf("expected error for ambiguous bare project ref; stderr=%s", stderr)
	}

	// 5. pull writes to per-env paths.
	if _, _, err := runCalypso(t, bin, pw, "--vault", vaultPath, "pull", "myapp@prod"); err != nil {
		t.Fatalf("pull @prod: %v", err)
	}
	data, _ := os.ReadFile(prodEnv)
	if !strings.Contains(string(data), "DB_HOST=db.prod.internal") {
		t.Errorf("prod .env content: %s", data)
	}

	// 6. diff across envs of the same project.
	stdout, _, err := runCalypso(t, bin, pw, "--vault", vaultPath,
		"diff", "myapp@dev", "myapp@prod", "--reveal")
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if !strings.Contains(stdout, "DB_HOST") || !strings.Contains(stdout, "differs") {
		t.Errorf("diff should show DB_HOST differs: %s", stdout)
	}
	if !strings.Contains(stdout, "DEBUG") || !strings.Contains(stdout, "only in myapp@dev") {
		t.Errorf("diff should show DEBUG only in dev: %s", stdout)
	}

	// 7. env list shows both envs.
	stdout, _, _ = runCalypso(t, bin, pw, "--vault", vaultPath, "env", "list", "myapp")
	if !strings.Contains(stdout, "dev") || !strings.Contains(stdout, "prod") {
		t.Errorf("env list should mention dev and prod: %s", stdout)
	}

	// 8. env copy bootstraps a staging env from dev.
	if _, stderr, err := runCalypso(t, bin, pw, "--vault", vaultPath,
		"env", "copy", "myapp@dev", "staging", "--path", stagingEnv); err != nil {
		t.Fatalf("env copy: %v\nstderr: %s", err, stderr)
	}
	stdout, _, _ = runCalypso(t, bin, pw, "--vault", vaultPath, "get", "myapp@staging", "DB_HOST", "--reveal")
	if !strings.Contains(stdout, "localhost") {
		t.Errorf("staging should have dev's DB_HOST=localhost, got: %s", stdout)
	}

	// 9. gaps detects intra-project drift: prod has CDN, dev/staging don't.
	stdout, _, err = runCalypso(t, bin, pw, "--vault", vaultPath, "gaps")
	if err != nil {
		t.Fatalf("gaps: %v", err)
	}
	if !strings.Contains(stdout, "Intra-project gaps") {
		t.Errorf("gaps should surface intra-project section: %s", stdout)
	}
	if !strings.Contains(stdout, "CDN") {
		t.Errorf("gaps should flag CDN missing from dev/staging: %s", stdout)
	}

	// 10. remove a single env (prod), then verify dev+staging remain.
	if _, stderr, err := runCalypso(t, bin, pw, "--vault", vaultPath,
		"remove", "myapp@prod", "--force"); err != nil {
		t.Fatalf("remove @prod: %v\nstderr: %s", err, stderr)
	}
	stdout, _, _ = runCalypso(t, bin, pw, "--vault", vaultPath, "env", "list", "myapp")
	if strings.Contains(stdout, "prod") {
		t.Errorf("prod should be gone, got: %s", stdout)
	}
	if !strings.Contains(stdout, "dev") || !strings.Contains(stdout, "staging") {
		t.Errorf("dev+staging should remain, got: %s", stdout)
	}
}

// TestIntegration_VaultDowngrade exercises the `calypso vault downgrade`
// CLI. The happy path needs a v2 file with v1-representable content, which
// we get by briefly having two envs, saving (forces v2), then removing
// the extra env (sticky-v2 keeps it). We then verify both the success and
// refusal codepaths.
func TestIntegration_VaultDowngrade(t *testing.T) {
	bin := calypsoPath(t)
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.enc")
	pw := "downgrade-test"

	// Setup: get the file to sticky-v2 with v1-representable content.
	runCalypso(t, bin, pw, "--vault", vaultPath, "add", "alpha", "--path", filepath.Join(dir, ".env"))
	runCalypso(t, bin, pw, "--vault", vaultPath, "add", "alpha@prod", "--path", filepath.Join(dir, ".env.prod"))
	if _, stderr, err := runCalypso(t, bin, pw, "--vault", vaultPath, "remove", "alpha@prod", "--force"); err != nil {
		t.Fatalf("remove prod: %v\nstderr: %s", err, stderr)
	}

	// Happy path: downgrade succeeds, backup is written.
	stdout, stderr, err := runCalypso(t, bin, pw, "--vault", vaultPath, "vault", "downgrade", "--force")
	if err != nil {
		t.Fatalf("vault downgrade: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "Downgraded") {
		t.Errorf("expected 'Downgraded' in output, got: %s", stdout)
	}
	if _, err := os.Stat(vaultPath + ".v2.bak"); err != nil {
		t.Errorf("backup file missing: %v", err)
	}

	// The downgraded file is still openable — round-trip via `get`.
	stdout, _, _ = runCalypso(t, bin, pw, "--vault", vaultPath, "list")
	if !strings.Contains(stdout, "alpha") {
		t.Errorf("list after downgrade should show alpha: %s", stdout)
	}

	// Refusal path: add a second env and try to downgrade — should fail
	// without touching the file.
	runCalypso(t, bin, pw, "--vault", vaultPath, "add", "alpha@prod", "--path", filepath.Join(dir, ".env.prod2"))
	preStat, _ := os.Stat(vaultPath)
	_, stderr, err = runCalypso(t, bin, pw, "--vault", vaultPath, "vault", "downgrade", "--force")
	if err == nil {
		t.Fatal("vault downgrade should fail when projects have multiple envs")
	}
	if !strings.Contains(stderr, "alpha") {
		t.Errorf("error should name the blocking project: %s", stderr)
	}
	postStat, _ := os.Stat(vaultPath)
	if !preStat.ModTime().Equal(postStat.ModTime()) {
		t.Error("vault file was modified despite refusal")
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
