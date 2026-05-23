package vault

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yemon/calypso/internal/crypto"
)

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
// is now v1-representable.
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
	if err := loaded.Save(bg, path, pw); err != nil {
		t.Fatal(err)
	}
	if got := onDiskVersion(t, path, pw); got != 2 {
		t.Errorf("expected file to stay v2 (sticky), got %d", got)
	}
}
