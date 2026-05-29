package vault

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yemon/calypso/internal/crypto"
	"github.com/yemon/calypso/internal/project"
)

// TestUnmarshalRejectsNewerVersion confirms a vault written by a future
// calypso (schema version above what this build supports) is refused at load
// with a clear message, rather than loaded and then failing on the next save.
func TestUnmarshalRejectsNewerVersion(t *testing.T) {
	_, err := unmarshalAndMigrate([]byte(`{"version":99,"projects":{}}`))
	if err == nil {
		t.Fatal("expected an error loading a newer-than-supported vault version")
	}
	if !strings.Contains(err.Error(), "newer") {
		t.Errorf("error should explain the version is too new, got: %v", err)
	}
}

// TestMigrateV1 writes a v1-shaped vault to disk, then loads it via the
// normal Load path and asserts each old project ends up with a single
// `default` env that preserves its path and vars. The post-load Save also
// confirms that the file stays in v1 on disk thanks to hold-the-line.
func TestMigrateV1(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")

	type v1Proj struct {
		Name      string        `json:"name"`
		Path      string        `json:"path"`
		Vars      []project.Var `json:"vars"`
		UpdatedAt string        `json:"updated_at"`
	}
	old := struct {
		Version   int                `json:"version"`
		Projects  map[string]*v1Proj `json:"projects"`
		CreatedAt string             `json:"created_at"`
	}{
		Version:   1,
		CreatedAt: "2024-01-01T00:00:00Z",
		Projects: map[string]*v1Proj{
			"alpha": {
				Name:      "alpha",
				Path:      "/tmp/alpha/.env",
				UpdatedAt: "2024-02-01T00:00:00Z",
				Vars: []project.Var{
					{Key: "DB_HOST", Value: "localhost"},
					{Key: "DB_PORT", Value: "5432"},
				},
			},
		},
	}
	plain, err := json.Marshal(old)
	if err != nil {
		t.Fatalf("marshal v1: %v", err)
	}
	blob, err := crypto.Encrypt(pw, plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatal(err)
	}

	v, err := Load(bg, path, pw)
	if err != nil {
		t.Fatalf("Load v1 vault: %v", err)
	}
	if v.Version != schemaVersion {
		t.Errorf("Load should upgrade Version in memory, got %d", v.Version)
	}
	p, e, err := v.ResolveEnv("alpha")
	if err != nil {
		t.Fatalf("ResolveEnv after migration: %v", err)
	}
	if e.Name != project.DefaultEnvName {
		t.Errorf("expected env %q, got %q", project.DefaultEnvName, e.Name)
	}
	if e.Path != "/tmp/alpha/.env" {
		t.Errorf("env path lost in migration: %q", e.Path)
	}
	if val, ok := e.Get("DB_HOST"); !ok || val != "localhost" {
		t.Errorf("DB_HOST after migration: ok=%v val=%q", ok, val)
	}
	if p.UpdatedAt != "2024-02-01T00:00:00Z" {
		t.Errorf("project UpdatedAt: got %q", p.UpdatedAt)
	}

	// Hold-the-line: a save that doesn't add a second env keeps the file
	// in v1, so old binaries can still open it.
	if err := v.Save(bg, path, pw); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := onDiskVersion(t, path, pw); got != 1 {
		t.Errorf("after Save with single default env, on-disk version should stay 1, got %d", got)
	}
}

func TestSaveAndLoadPreservesProjects(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	v, _ := Init(bg, path, pw)

	_, e, _ := v.AddProject("svc", "dev", "/tmp/svc/.env")
	e.Set("KEY", "value")
	v.Touch("svc", "dev")

	if err := v.Save(bg, path, pw); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, _ := Load(bg, path, pw)
	_, le, err := loaded.ResolveEnv("svc")
	if err != nil {
		t.Fatalf("ResolveEnv: %v", err)
	}
	val, ok := le.Get("KEY")
	if !ok {
		t.Fatal("KEY not found after reload")
	}
	if val != "value" {
		t.Errorf("expected 'value', got %q", val)
	}
}
