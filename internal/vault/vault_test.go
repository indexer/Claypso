package vault

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/yemon/calypso/internal/project"
)

var bg = context.Background()

func vaultFile(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "vault.enc")
}

func TestInitAndLoad(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("super secret")

	v, err := Init(bg, path, pw)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if v.Version != schemaVersion {
		t.Errorf("version: expected %d, got %d", schemaVersion, v.Version)
	}
	if len(v.Projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(v.Projects))
	}

	loaded, err := Load(bg, path, pw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Version != v.Version {
		t.Errorf("loaded version mismatch")
	}
}

// TestSaveReusesLoadedSalt verifies the key-caching optimization: after a
// Load, saving again reuses the on-disk salt (and therefore the already-derived
// key) instead of generating a new salt and re-running Argon2id. The salt is
// the first 16 bytes of the encrypted file; it must be stable across saves
// within the same in-memory vault.
func TestSaveReusesLoadedSalt(t *testing.T) {
	const saltLen = 16
	path := vaultFile(t)
	pw := []byte("super secret")

	if _, err := Init(bg, path, pw); err != nil {
		t.Fatalf("Init: %v", err)
	}

	loaded, err := Load(bg, path, pw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.cipher == nil {
		t.Fatal("Load should populate the cached cipher")
	}

	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read vault: %v", err)
	}
	saltBefore := string(blob[:saltLen])

	if _, _, err := loaded.AddProject("p", "", "/tmp/p/.env"); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if err := loaded.Save(bg, path, pw); err != nil {
		t.Fatalf("Save: %v", err)
	}

	blob, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-read vault: %v", err)
	}
	if string(blob[:saltLen]) != saltBefore {
		t.Error("salt changed after save: cipher was not reused (key re-derived)")
	}

	// Sanity: the resaved vault still decrypts with the same passphrase.
	if _, err := Load(bg, path, pw); err != nil {
		t.Fatalf("reload after resave: %v", err)
	}
}

func TestInitFailsIfExists(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pass")
	if _, err := Init(bg, path, pw); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	_, err := Init(bg, path, pw)
	if err != ErrExists {
		t.Errorf("expected ErrExists, got %v", err)
	}
}

func TestLoadNotFound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent", "vault.enc")
	_, err := Load(bg, path, []byte("pw"))
	if err != ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestLoadWrongPassphrase(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("correct")
	if _, err := Init(bg, path, pw); err != nil {
		t.Fatalf("Init: %v", err)
	}
	_, err := Load(bg, path, []byte("wrong"))
	if err == nil {
		t.Fatal("expected error for wrong passphrase")
	}
}

func TestAddProject(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	v, _ := Init(bg, path, pw)

	p, e, err := v.AddProject("myapp", "", "test.env")
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if p.Name != "myapp" {
		t.Errorf("name: expected myapp, got %s", p.Name)
	}
	if e.Name != project.DefaultEnvName {
		t.Errorf("env name: expected %q, got %q", project.DefaultEnvName, e.Name)
	}
	if e.Path == "test.env" {
		t.Error("path should be absolute, not relative")
	}

	if _, _, err := v.AddProject("myapp", "", "other.env"); err == nil {
		t.Error("expected error for duplicate project")
	}
}

func TestAddEnvToProject(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	v, _ := Init(bg, path, pw)

	if _, _, err := v.AddProject("myapp", "dev", "/tmp/.env.dev"); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	e, err := v.AddEnvToProject("myapp", "prod", "/tmp/.env.prod")
	if err != nil {
		t.Fatalf("AddEnvToProject: %v", err)
	}
	if e.Name != "prod" {
		t.Errorf("expected env prod, got %s", e.Name)
	}
	if _, err := v.AddEnvToProject("myapp", "prod", "/tmp/x"); err == nil {
		t.Error("duplicate env should error")
	}
	if _, err := v.AddEnvToProject("nonexistent", "x", "/tmp/x"); err == nil {
		t.Error("unknown project should error")
	}
}

func TestProjectNotFound(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	v, _ := Init(bg, path, pw)

	if _, err := v.Project("nonexistent"); err == nil {
		t.Error("expected error")
	}
}

func TestRemoveProject(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	v, _ := Init(bg, path, pw)

	v.AddProject("app", "", "/tmp/.env")
	if len(v.Projects) != 1 {
		t.Fatalf("expected 1 project")
	}

	if err := v.RemoveProject("app"); err != nil {
		t.Fatalf("RemoveProject: %v", err)
	}
	if len(v.Projects) != 0 {
		t.Error("expected 0 projects after remove")
	}

	if err := v.RemoveProject("nonexistent"); err == nil {
		t.Error("expected error removing nonexistent project")
	}
}

func TestRemoveEnv(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	v, _ := Init(bg, path, pw)

	v.AddProject("app", "dev", "/tmp/.env.dev")
	v.AddEnvToProject("app", "prod", "/tmp/.env.prod")

	if err := v.RemoveEnv("app", "dev"); err != nil {
		t.Fatalf("RemoveEnv dev: %v", err)
	}
	if err := v.RemoveEnv("app", "prod"); err == nil {
		t.Error("removing last env should fail")
	}
	if err := v.RemoveEnv("app", "ghost"); err == nil {
		t.Error("unknown env should fail")
	}
}

func TestNamesSorted(t *testing.T) {
	v := New()
	v.Projects["zulu"] = &project.Project{Name: "zulu", Envs: map[string]*project.Environment{}}
	v.Projects["alpha"] = &project.Project{Name: "alpha", Envs: map[string]*project.Environment{}}
	v.Projects["mike"] = &project.Project{Name: "mike", Envs: map[string]*project.Environment{}}

	names := v.Names()
	expected := []string{"alpha", "mike", "zulu"}
	for i, n := range names {
		if n != expected[i] {
			t.Errorf("names[%d]: expected %q, got %q", i, expected[i], n)
		}
	}
}

func TestTouch(t *testing.T) {
	v := New()
	v.Projects["test"] = &project.Project{
		Name: "test",
		Envs: map[string]*project.Environment{
			"default": {Name: "default"},
		},
	}
	before := v.Projects["test"].UpdatedAt
	v.Touch("test", "default")
	after := v.Projects["test"].UpdatedAt
	if before == after {
		t.Error("Touch should update UpdatedAt")
	}

	v.Touch("nonexistent", "default")
}

func TestVaultFilePermissions(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	if _, err := Init(bg, path, pw); err != nil {
		t.Fatalf("Init: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("vault file should be 0600, got %#o", info.Mode().Perm())
	}
}

func TestSaveAtomic(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	if _, err := Init(bg, path, pw); err != nil {
		t.Fatalf("Init: %v", err)
	}

	tmpPath := path + ".tmp"
	if _, err := os.Stat(tmpPath); err == nil {
		t.Error("temp file should not exist after save + rename")
	}
}
