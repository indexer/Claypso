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

	p, err := v.AddProject("myapp", "test.env")
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if p.Name != "myapp" {
		t.Errorf("name: expected myapp, got %s", p.Name)
	}
	if p.Path == "test.env" {
		t.Error("path should be absolute, not relative")
	}

	_, err = v.AddProject("myapp", "other.env")
	if err == nil {
		t.Error("expected error for duplicate project")
	}
}

func TestProjectNotFound(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	v, _ := Init(bg, path, pw)

	_, err := v.Project("nonexistent")
	if err == nil {
		t.Error("expected error")
	}
}

func TestRemoveProject(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	v, _ := Init(bg, path, pw)

	v.AddProject("app", "/tmp/.env")
	if len(v.Projects) != 1 {
		t.Fatalf("expected 1 project")
	}

	if err := v.RemoveProject("app"); err != nil {
		t.Fatalf("RemoveProject: %v", err)
	}
	if len(v.Projects) != 0 {
		t.Error("expected 0 projects after remove")
	}

	err := v.RemoveProject("nonexistent")
	if err == nil {
		t.Error("expected error removing nonexistent project")
	}
}

func TestSaveAndLoadPreservesProjects(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	v, _ := Init(bg, path, pw)

	p, _ := v.AddProject("svc", "/tmp/svc/.env")
	p.Set("KEY", "value")
	v.Touch("svc")

	if err := v.Save(bg, path, pw); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, _ := Load(bg, path, pw)
	lp, _ := loaded.Project("svc")
	val, ok := lp.Get("KEY")
	if !ok {
		t.Fatal("KEY not found after reload")
	}
	if val != "value" {
		t.Errorf("expected 'value', got %q", val)
	}
}

func TestNamesSorted(t *testing.T) {
	v := New()
	v.Projects["zulu"] = &project.Project{Name: "zulu"}
	v.Projects["alpha"] = &project.Project{Name: "alpha"}
	v.Projects["mike"] = &project.Project{Name: "mike"}

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
	v.Projects["test"] = &project.Project{Name: "test"}
	before := v.Projects["test"].UpdatedAt
	v.Touch("test")
	after := v.Projects["test"].UpdatedAt
	if before == after {
		t.Error("Touch should update UpdatedAt")
	}

	v.Touch("nonexistent")
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
