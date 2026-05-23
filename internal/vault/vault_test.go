package vault

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yemon/calypso/internal/crypto"
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

	_, _, err = v.AddProject("myapp", "", "other.env")
	if err == nil {
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

	_, err := v.Project("nonexistent")
	if err == nil {
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

func TestParseSpec(t *testing.T) {
	cases := []struct {
		in        string
		wantP     string
		wantEnv   string
		wantError bool
	}{
		{"myapp", "myapp", "", false},
		{"myapp@prod", "myapp", "prod", false},
		{"myapp@dev-1", "myapp", "dev-1", false},
		{"", "", "", true},
		{"@prod", "", "", true},
		{"myapp@", "", "", true},
		{"myapp@PROD", "", "", true}, // uppercase disallowed
		{"myapp@dev/staging", "", "", true},
	}
	for _, tc := range cases {
		s, err := ParseSpec(tc.in)
		if tc.wantError {
			if err == nil {
				t.Errorf("ParseSpec(%q): expected error, got %+v", tc.in, s)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSpec(%q): unexpected error %v", tc.in, err)
			continue
		}
		if s.Project != tc.wantP || s.Env != tc.wantEnv {
			t.Errorf("ParseSpec(%q): got {%q,%q}, want {%q,%q}", tc.in, s.Project, s.Env, tc.wantP, tc.wantEnv)
		}
	}
}

func TestResolveEnvImplicitWhenSingle(t *testing.T) {
	v := New()
	v.AddProject("myapp", "dev", "/tmp/.env.dev")

	p, e, err := v.ResolveEnv("myapp")
	if err != nil {
		t.Fatalf("ResolveEnv with sole env should succeed: %v", err)
	}
	if p.Name != "myapp" || e.Name != "dev" {
		t.Errorf("got %s@%s, want myapp@dev", p.Name, e.Name)
	}
}

func TestResolveEnvRequiresExplicitWhenMultiple(t *testing.T) {
	v := New()
	v.AddProject("myapp", "dev", "/tmp/.env.dev")
	v.AddEnvToProject("myapp", "prod", "/tmp/.env.prod")

	if _, _, err := v.ResolveEnv("myapp"); err == nil {
		t.Error("ResolveEnv should require @env when multiple envs exist")
	}
	if _, e, err := v.ResolveEnv("myapp@prod"); err != nil || e.Name != "prod" {
		t.Errorf("explicit @prod failed: env=%+v err=%v", e, err)
	}
	if _, _, err := v.ResolveEnv("myapp@ghost"); err == nil {
		t.Error("ResolveEnv should error on unknown env")
	}
}

// TestMigrateV1 writes a v1-shaped vault to disk, then loads it via the
// normal Load path and asserts each old project ends up with a single
// `default` env that preserves its path and vars.
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

// onDiskVersion decrypts the vault file with pw and reports the `version`
// field of the JSON envelope. Used by the schema-stickiness tests.
func onDiskVersion(t *testing.T, path string, pw []byte) int {
	t.Helper()
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", path, err)
	}
	plain, err := crypto.Decrypt(pw, blob)
	if err != nil {
		t.Fatalf("Decrypt %s: %v", path, err)
	}
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(plain, &head); err != nil {
		t.Fatalf("unmarshal head: %v", err)
	}
	return head.Version
}

// TestHoldTheLine_SingleDefaultEnvStaysV1 confirms a brand-new vault with
// only single-env projects named "default" is written in v1 shape — so a
// user who never adopts multi-env can still open the file with an older
// binary.
func TestHoldTheLine_SingleDefaultEnvStaysV1(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	v, err := Init(bg, path, pw)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, _, err := v.AddProject("alpha", "", "/tmp/alpha/.env"); err != nil {
		t.Fatal(err)
	}
	if err := v.Save(bg, path, pw); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := onDiskVersion(t, path, pw); got != 1 {
		t.Errorf("single default env should write as v1, got %d", got)
	}
}

// TestHoldTheLine_MultiEnvWritesV2 confirms the moment a project has a
// second env, the file is written in v2.
func TestHoldTheLine_MultiEnvWritesV2(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	v, _ := Init(bg, path, pw)
	v.AddProject("alpha", "dev", "/tmp/.env.dev")
	if _, err := v.AddEnvToProject("alpha", "prod", "/tmp/.env.prod"); err != nil {
		t.Fatal(err)
	}
	if err := v.Save(bg, path, pw); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := onDiskVersion(t, path, pw); got != 2 {
		t.Errorf("multi-env vault should write as v2, got %d", got)
	}
}

// TestHoldTheLine_NonDefaultEnvNameForcesV2 confirms that a single env with
// a name other than "default" can't round-trip through v1 (which has no
// env-name concept), so we write v2.
func TestHoldTheLine_NonDefaultEnvNameForcesV2(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	v, _ := Init(bg, path, pw)
	v.AddProject("alpha", "dev", "/tmp/.env.dev")
	if err := v.Save(bg, path, pw); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := onDiskVersion(t, path, pw); got != 2 {
		t.Errorf("single env named %q should write as v2, got %d", "dev", got)
	}
}

// TestHoldTheLine_BackupOnUpgrade confirms that the first save which
// changes the on-disk schema version copies the previous file to a
// versioned .bak file before overwriting.
func TestHoldTheLine_BackupOnUpgrade(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")

	// Seed a v1 file on disk (same wire format the old binary would write).
	old := v1Vault{
		Version:   1,
		CreatedAt: "2024-01-01T00:00:00Z",
		Projects: map[string]*v1Project{
			"alpha": {Name: "alpha", Path: "/tmp/alpha/.env", UpdatedAt: "2024-02-01T00:00:00Z"},
		},
	}
	plain, _ := json.Marshal(old)
	blob, _ := crypto.Encrypt(pw, plain)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, blob, 0o600); err != nil {
		t.Fatal(err)
	}
	originalBlob := append([]byte(nil), blob...)

	// Load, add a second env (forces v2), save.
	v, err := Load(bg, path, pw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, err := v.AddEnvToProject("alpha", "prod", "/tmp/.env.prod"); err != nil {
		t.Fatal(err)
	}
	if err := v.Save(bg, path, pw); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if got := onDiskVersion(t, path, pw); got != 2 {
		t.Errorf("after upgrade, file should be v2, got %d", got)
	}

	bak := path + ".v1.bak"
	got, err := os.ReadFile(bak)
	if err != nil {
		t.Fatalf("backup file missing: %v", err)
	}
	if string(got) != string(originalBlob) {
		t.Error("backup content does not match pre-upgrade blob")
	}

	// A subsequent save should NOT clobber the backup (loadedVersion is now
	// 2, no further format change to back up against).
	mtimeBefore, _ := os.Stat(bak)
	if err := v.Save(bg, path, pw); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	mtimeAfter, _ := os.Stat(bak)
	if !mtimeBefore.ModTime().Equal(mtimeAfter.ModTime()) {
		t.Error("backup file was overwritten on a save that did not change format")
	}
}

// TestHoldTheLine_StickyV2 confirms that once a vault has been observed
// as v2 on disk, later saves stay at v2 even if the in-memory content
// is now v1-representable. Downgrading silently would be a surprising
// data-loss footgun (the v2 file may have richer state that an
// intervening user added).
func TestHoldTheLine_StickyV2(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	v, _ := Init(bg, path, pw)
	v.AddProject("alpha", "dev", "/tmp/.env.dev")
	v.AddEnvToProject("alpha", "prod", "/tmp/.env.prod")
	if err := v.Save(bg, path, pw); err != nil {
		t.Fatal(err)
	}
	if got := onDiskVersion(t, path, pw); got != 2 {
		t.Fatalf("setup: expected v2 on disk, got %d", got)
	}

	loaded, err := Load(bg, path, pw)
	if err != nil {
		t.Fatal(err)
	}
	if err := loaded.RemoveEnv("alpha", "prod"); err != nil {
		t.Fatal(err)
	}
	// Even though "alpha" now has just env "dev", we should not auto-downgrade.
	if err := loaded.Save(bg, path, pw); err != nil {
		t.Fatal(err)
	}
	if got := onDiskVersion(t, path, pw); got != 2 {
		t.Errorf("expected file to stay v2 (sticky), got %d", got)
	}
}

// TestV1Blockers covers the matrix of v1-incompatible states.
func TestV1Blockers(t *testing.T) {
	t.Run("clean single default env", func(t *testing.T) {
		v := New()
		v.AddProject("alpha", "", "/tmp/.env") // env name defaults to "default"
		if got := v.V1Blockers(); len(got) != 0 {
			t.Errorf("expected no blockers, got %v", got)
		}
	})

	t.Run("multiple envs", func(t *testing.T) {
		v := New()
		v.AddProject("alpha", "", "/tmp/.env")
		v.AddEnvToProject("alpha", "prod", "/tmp/.env.prod")
		got := v.V1Blockers()
		if len(got) != 1 {
			t.Fatalf("expected 1 blocker, got %v", got)
		}
		if !strings.Contains(got[0], "alpha") || !strings.Contains(got[0], "2 envs") {
			t.Errorf("blocker should mention project and env count: %q", got[0])
		}
	})

	t.Run("single env with custom name", func(t *testing.T) {
		v := New()
		v.AddProject("alpha", "dev", "/tmp/.env")
		got := v.V1Blockers()
		if len(got) != 1 {
			t.Fatalf("expected 1 blocker, got %v", got)
		}
		if !strings.Contains(got[0], "alpha") || !strings.Contains(got[0], `"dev"`) {
			t.Errorf("blocker should mention project and env name: %q", got[0])
		}
	})

	t.Run("multiple projects, multiple blockers", func(t *testing.T) {
		v := New()
		v.AddProject("alpha", "dev", "/tmp/a.env")
		v.AddProject("beta", "", "/tmp/b.env")
		v.AddEnvToProject("beta", "prod", "/tmp/b.env.prod")
		got := v.V1Blockers()
		if len(got) != 2 {
			t.Errorf("expected 2 blockers, got %v", got)
		}
	})
}

// TestDowngrade_HappyPath verifies that a v2 file whose content is
// v1-representable can be explicitly rewritten back to v1, and that the
// pre-downgrade blob is preserved at <path>.v2.bak.
func TestDowngrade_HappyPath(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	v, _ := Init(bg, path, pw)
	// Force the file to v2 by briefly having two envs, then remove one.
	v.AddProject("alpha", "", "/tmp/.env")
	v.AddEnvToProject("alpha", "prod", "/tmp/.env.prod")
	if err := v.Save(bg, path, pw); err != nil {
		t.Fatal(err)
	}
	if got := onDiskVersion(t, path, pw); got != 2 {
		t.Fatalf("setup: expected v2, got %d", got)
	}
	if err := v.RemoveEnv("alpha", "prod"); err != nil {
		t.Fatal(err)
	}
	// Sticky-v2 keeps the file at v2 even though it could be v1 now.
	if err := v.Save(bg, path, pw); err != nil {
		t.Fatal(err)
	}
	if got := onDiskVersion(t, path, pw); got != 2 {
		t.Fatalf("setup: expected sticky v2, got %d", got)
	}
	preBlob, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	// Explicit downgrade — what the user runs.
	if err := v.Downgrade(bg, path, pw); err != nil {
		t.Fatalf("Downgrade: %v", err)
	}
	if got := onDiskVersion(t, path, pw); got != 1 {
		t.Errorf("after Downgrade, on-disk version should be 1, got %d", got)
	}
	bak, err := os.ReadFile(path + ".v2.bak")
	if err != nil {
		t.Fatalf("backup file missing: %v", err)
	}
	if string(bak) != string(preBlob) {
		t.Error("v2 backup does not match pre-downgrade blob")
	}

	// Round-trip: load the downgraded file and confirm content is intact.
	reloaded, err := Load(bg, path, pw)
	if err != nil {
		t.Fatalf("re-Load after downgrade: %v", err)
	}
	if _, _, err := reloaded.ResolveEnv("alpha"); err != nil {
		t.Errorf("ResolveEnv after downgrade: %v", err)
	}
}

// TestDowngrade_RefusesMultiEnv confirms the safety check fires before any
// disk write when the vault has multi-env projects.
func TestDowngrade_RefusesMultiEnv(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	v, _ := Init(bg, path, pw)
	v.AddProject("alpha", "", "/tmp/.env")
	v.AddEnvToProject("alpha", "prod", "/tmp/.env.prod")
	if err := v.Save(bg, path, pw); err != nil {
		t.Fatal(err)
	}
	preBlob, _ := os.ReadFile(path)

	err := v.Downgrade(bg, path, pw)
	if err == nil {
		t.Fatal("Downgrade should refuse when v1 blockers exist")
	}
	if !strings.Contains(err.Error(), "alpha") {
		t.Errorf("error should name the blocking project: %v", err)
	}

	// The on-disk file must be untouched.
	post, _ := os.ReadFile(path)
	if string(post) != string(preBlob) {
		t.Error("file was modified despite refusal")
	}
	// And no spurious backup was created.
	if _, err := os.Stat(path + ".v2.bak"); err == nil {
		t.Error("backup file should not exist when Downgrade refused")
	}
}

// TestDowngrade_RefusesCustomEnvName mirrors the custom-name path through
// V1Blockers — a single env named other than "default" can't round-trip.
func TestDowngrade_RefusesCustomEnvName(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	v, _ := Init(bg, path, pw)
	v.AddProject("alpha", "dev", "/tmp/.env.dev")
	if err := v.Save(bg, path, pw); err != nil {
		t.Fatal(err)
	}

	if err := v.Downgrade(bg, path, pw); err == nil {
		t.Fatal("Downgrade should refuse custom env name")
	}
}
