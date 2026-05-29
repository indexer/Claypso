package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegration_Version(t *testing.T) {
	bin := calypsoPath(t)
	stdout, stderr, err := runCalypso(t, bin, "", "version")
	if err != nil {
		t.Fatalf("version: %v\nstderr: %s", err, stderr)
	}
	for _, want := range []string{"calypso", "commit:", "go:"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("version output %q should contain %q", stdout, want)
		}
	}
}

func TestIntegration_VaultVerify(t *testing.T) {
	bin := calypsoPath(t)
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.enc")
	envPath := filepath.Join(dir, "app", ".env")
	os.MkdirAll(filepath.Dir(envPath), 0o700)
	os.WriteFile(envPath, []byte("A=1\n"), 0o600)

	pw := "verify-test"
	runCalypso(t, bin, pw, "--vault", vaultPath, "add", "app", "--path", envPath)
	runCalypso(t, bin, pw, "--vault", vaultPath, "push", "app", "--force")

	// A healthy vault: verify exits 0 and reports no errors.
	stdout, stderr, err := runCalypso(t, bin, pw, "--vault", vaultPath, "vault", "verify")
	if err != nil {
		t.Fatalf("vault verify on a healthy vault should exit 0: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout+stderr, "Findings") {
		t.Errorf("verify should print a findings section: %s", stdout+stderr)
	}

	// --fix on a healthy vault: nothing to repair, still exits 0.
	stdout, stderr, err = runCalypso(t, bin, pw, "--vault", vaultPath, "vault", "verify", "--fix")
	if err != nil {
		t.Fatalf("vault verify --fix should exit 0: %v\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout+stderr, "No fixable issues") {
		t.Errorf("verify --fix on a healthy vault should report nothing to fix: %s", stdout+stderr)
	}
}

func TestIntegration_Drift(t *testing.T) {
	bin := calypsoPath(t)
	dir := t.TempDir()
	vaultPath := filepath.Join(dir, "vault.enc")
	envPath := filepath.Join(dir, "app", ".env")
	os.MkdirAll(filepath.Dir(envPath), 0o700)
	os.WriteFile(envPath, []byte("A=1\nB=2\n"), 0o600)

	pw := "drift-test"
	runCalypso(t, bin, pw, "--vault", vaultPath, "add", "app", "--path", envPath)
	runCalypso(t, bin, pw, "--vault", vaultPath, "push", "app", "--force")

	// Vault matches disk → no drift, exit 0.
	if _, stderr, err := runCalypso(t, bin, pw, "--vault", vaultPath, "drift", "app"); err != nil {
		t.Fatalf("drift on a synced env should exit 0: %v\nstderr: %s", err, stderr)
	}

	// Edit the .env on disk → drift detected → non-zero exit, key reported.
	os.WriteFile(envPath, []byte("A=1\nB=2\nC=3\n"), 0o600)
	stdout, stderr, err := runCalypso(t, bin, pw, "--vault", vaultPath, "drift", "app", "--details")
	if err == nil {
		t.Error("drift should exit non-zero when the .env diverges from the vault")
	}
	if !strings.Contains(stdout+stderr, "C") {
		t.Errorf("drift --details should report the diverging key C: %s", stdout+stderr)
	}
}

func TestIntegration_ImportMerge(t *testing.T) {
	bin := calypsoPath(t)
	dir := t.TempDir()
	pw := "merge-test"

	// Source vault with project "shared".
	srcVault := filepath.Join(dir, "src.enc")
	srcEnv := filepath.Join(dir, "shared", ".env")
	os.MkdirAll(filepath.Dir(srcEnv), 0o700)
	os.WriteFile(srcEnv, []byte("TOKEN=abc\n"), 0o600)
	runCalypso(t, bin, pw, "--vault", srcVault, "add", "shared", "--path", srcEnv)
	runCalypso(t, bin, pw, "--vault", srcVault, "push", "shared", "--force")

	// Export just that project to a partial-export blob.
	exportPath := filepath.Join(dir, "shared.cbk")
	if _, stderr, err := runCalypso(t, bin, pw, "--vault", srcVault, "vault", "export-project", "shared", exportPath); err != nil {
		t.Fatalf("export-project: %v\nstderr: %s", err, stderr)
	}

	// Destination vault with a different project.
	dstVault := filepath.Join(dir, "dst.enc")
	dstEnv := filepath.Join(dir, "other", ".env")
	os.MkdirAll(filepath.Dir(dstEnv), 0o700)
	os.WriteFile(dstEnv, []byte("X=1\n"), 0o600)
	runCalypso(t, bin, pw, "--vault", dstVault, "add", "other", "--path", dstEnv)
	runCalypso(t, bin, pw, "--vault", dstVault, "push", "other", "--force")

	// Merge the partial export in: destination should then hold both projects.
	if _, stderr, err := runCalypso(t, bin, pw, "--vault", dstVault, "import", exportPath, "--merge"); err != nil {
		t.Fatalf("import --merge: %v\nstderr: %s", err, stderr)
	}
	stdout, _, err := runCalypso(t, bin, pw, "--vault", dstVault, "list")
	if err != nil {
		t.Fatalf("list after merge: %v", err)
	}
	if !strings.Contains(stdout, "shared") || !strings.Contains(stdout, "other") {
		t.Errorf("merged vault should contain both 'shared' and 'other': %s", stdout)
	}

	// Re-merging an existing project without --force must fail (collision).
	if _, _, err := runCalypso(t, bin, pw, "--vault", dstVault, "import", exportPath, "--merge"); err == nil {
		t.Error("re-merging an existing project without --force should fail")
	}
	// With --force it succeeds (replaces the project).
	if _, stderr, err := runCalypso(t, bin, pw, "--vault", dstVault, "import", exportPath, "--merge", "--force"); err != nil {
		t.Fatalf("import --merge --force should replace: %v\nstderr: %s", err, stderr)
	}
}
