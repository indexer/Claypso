package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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
