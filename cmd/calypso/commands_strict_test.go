package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yemon/calypso/internal/project"
	"github.com/yemon/calypso/internal/vault"
)

func TestSanitizeRegisteredEnvFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	v := vault.New()
	_, env, err := v.AddProject("web", "", path)
	if err != nil {
		t.Fatal(err)
	}
	env.Set("TOKEN", "runtime-secret")
	if err := project.WriteSafeEnvFile(path, env.Vars); err != nil {
		t.Fatal(err)
	}
	if err := sanitizeRegisteredEnvFiles(v); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "TOKEN") || strings.Contains(string(data), "runtime-secret") || strings.Contains(string(data), "=") {
		t.Fatal("strict sanitization retained credential material")
	}
}

func TestSanitizeRegisteredEnvFilesRefusesUnsyncedEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	v := vault.New()
	_, env, err := v.AddProject("web", "", path)
	if err != nil {
		t.Fatal(err)
	}
	env.Set("TOKEN", "vault-value")
	const local = "TOKEN=local-unsynced\n"
	if err := os.WriteFile(path, []byte(local), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sanitizeRegisteredEnvFiles(v); err == nil {
		t.Fatal("strict sanitization should refuse unsynced edits")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != local {
		t.Fatal("strict sanitization modified an unsynced file")
	}
}
