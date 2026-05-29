package vault

import (
	"os"
	"testing"
)

// TestImportRawBlob covers the unattended full-import path: it must take the
// lock, write atomically (no leftover temp), and produce a vault that decrypts
// back to the original contents.
func TestImportRawBlob(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	v, err := Init(bg, path, pw)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if _, _, err := v.AddProject("alpha", "", "/tmp/alpha/.env"); err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	if err := v.Save(bg, path, pw); err != nil {
		t.Fatalf("Save: %v", err)
	}
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read blob: %v", err)
	}

	// Wipe the vault, then re-import the raw blob over the (now absent) path.
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := ImportRawBlob(path, blob); err != nil {
		t.Fatalf("ImportRawBlob: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("atomic import should leave no temp file (stat err=%v)", err)
	}

	loaded, err := Load(bg, path, pw)
	if err != nil {
		t.Fatalf("Load after import: %v", err)
	}
	if _, err := loaded.Project("alpha"); err != nil {
		t.Errorf("imported vault is missing its project: %v", err)
	}
}
