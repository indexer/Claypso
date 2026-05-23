package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

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

	stdout, _, err = runCalypso(t, bin, pw, "--vault", vault2, "list")
	if err != nil {
		t.Fatalf("list imported: %v", err)
	}
	if !strings.Contains(stdout, "app") {
		t.Errorf("imported vault should contain 'app': %s", stdout)
	}
}
