package analysis

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yemon/calypso/internal/project"
)

func exposureEnv(t *testing.T, dir, content string, vars ...string) *project.Environment {
	t.Helper()
	path := filepath.Join(dir, ".env")
	if content != "" {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write env: %v", err)
		}
	}
	e := &project.Environment{Name: "dev", Path: path}
	for i := 0; i+1 < len(vars); i += 2 {
		e.Vars = append(e.Vars, project.Var{Key: vars[i], Value: project.SecretFromString(vars[i+1])})
	}
	return e
}

func TestExposure_HotWhenDiskMatchesVault(t *testing.T) {
	e := exposureEnv(t, t.TempDir(),
		"API_KEY=supersecretvalue\nDB_HOST=localhost\n",
		"API_KEY", "supersecretvalue", "DB_HOST", "localhost")
	rep, err := Exposure(e, "myapp", time.Now())
	if err != nil {
		t.Fatalf("Exposure: %v", err)
	}
	if !rep.Hot() {
		t.Fatal("expected hot report")
	}
	if len(rep.HotKeys) != 2 {
		t.Errorf("want 2 hot keys, got %v", rep.HotKeys)
	}
	if rep.Age < 0 || rep.Age > time.Minute {
		t.Errorf("age should be ~0 for a freshly written file, got %v", rep.Age)
	}
}

func TestExposure_MaskedFileNotHot(t *testing.T) {
	e := exposureEnv(t, t.TempDir(),
		"API_KEY=****\nDB_HOST=****\n",
		"API_KEY", "supersecretvalue", "DB_HOST", "localhost")
	rep, err := Exposure(e, "myapp", time.Now())
	if err != nil {
		t.Fatalf("Exposure: %v", err)
	}
	if rep.Hot() {
		t.Errorf("masked file should not be hot, got %v", rep.HotKeys)
	}
}

func TestExposure_ShortValuesIgnored(t *testing.T) {
	// DEBUG=1 matches the vault but is below MinExposedLen — not a secret.
	e := exposureEnv(t, t.TempDir(), "DEBUG=1\n", "DEBUG", "1")
	rep, err := Exposure(e, "myapp", time.Now())
	if err != nil {
		t.Fatalf("Exposure: %v", err)
	}
	if rep.Hot() {
		t.Errorf("short values should be ignored, got %v", rep.HotKeys)
	}
}

func TestExposure_RotatedValueNotHot(t *testing.T) {
	// Disk holds an old/hand-edited value that no longer matches the vault.
	e := exposureEnv(t, t.TempDir(), "API_KEY=old-rotated-away\n", "API_KEY", "supersecretvalue")
	rep, err := Exposure(e, "myapp", time.Now())
	if err != nil {
		t.Fatalf("Exposure: %v", err)
	}
	if rep.Hot() {
		t.Errorf("non-matching disk value should not be hot, got %v", rep.HotKeys)
	}
}

func TestExposure_MissingFile(t *testing.T) {
	e := exposureEnv(t, t.TempDir(), "", "API_KEY", "supersecretvalue")
	rep, err := Exposure(e, "myapp", time.Now())
	if err != nil {
		t.Fatalf("Exposure: %v", err)
	}
	if !rep.Missing || rep.Hot() {
		t.Errorf("missing file should be Missing and not hot: %+v", rep)
	}
}

func TestExposure_AgeFromMtime(t *testing.T) {
	dir := t.TempDir()
	e := exposureEnv(t, dir, "API_KEY=supersecretvalue\n", "API_KEY", "supersecretvalue")
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(e.Path, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	rep, err := Exposure(e, "myapp", time.Now())
	if err != nil {
		t.Fatalf("Exposure: %v", err)
	}
	if rep.Age < 2*time.Hour-time.Minute {
		t.Errorf("expected ~2h age, got %v", rep.Age)
	}
}
