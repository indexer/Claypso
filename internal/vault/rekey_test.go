package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/yemon/calypso/internal/crypto"
	"github.com/yemon/calypso/internal/project"
)

func TestRekey(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "vault.enc")
	oldPw := []byte("old-passphrase-1")
	newPw := []byte("new-passphrase-2")

	v, err := Init(ctx, path, oldPw)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	v.Projects["myapp"] = &project.Project{
		Name: "myapp",
		Envs: map[string]*project.Environment{project.DefaultEnvName: {
			Name: project.DefaultEnvName,
			Path: "/tmp/.env",
			Vars: []project.Var{{Key: "API_KEY", Value: project.SecretFromString("supersecretvalue")}},
		}},
	}
	if err := v.Save(ctx, path, oldPw); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := v.Rekey(ctx, path, newPw); err != nil {
		t.Fatalf("Rekey: %v", err)
	}

	// New passphrase opens the vault, data intact.
	loaded, err := Load(ctx, path, newPw)
	if err != nil {
		t.Fatalf("Load with new passphrase: %v", err)
	}
	got, ok := loaded.Projects["myapp"].Envs[project.DefaultEnvName].Get("API_KEY")
	if !ok || got != "supersecretvalue" {
		t.Errorf("data lost across rekey: %q, %v", got, ok)
	}

	// Old passphrase no longer works.
	if _, err := Load(ctx, path, oldPw); !errors.Is(err, crypto.ErrDecrypt) {
		t.Errorf("old passphrase should fail with ErrDecrypt, got %v", err)
	}

	// Pre-rekey backup exists and still opens with the old passphrase.
	bak := path + ".pre-rekey.bak"
	if _, err := os.Stat(bak); err != nil {
		t.Fatalf("pre-rekey backup missing: %v", err)
	}
	if _, err := Load(ctx, bak, oldPw); err != nil {
		t.Errorf("pre-rekey backup should decrypt with the old passphrase: %v", err)
	}

	// Subsequent saves in the same process use the new cipher.
	v.Touch("myapp", project.DefaultEnvName)
	if err := v.Save(ctx, path, newPw); err != nil {
		t.Fatalf("Save after rekey: %v", err)
	}
	if _, err := Load(ctx, path, newPw); err != nil {
		t.Errorf("vault unreadable after post-rekey save: %v", err)
	}
}

func TestLockdownUnattendedOKPersistsAndForcesV2(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "vault.enc")
	pw := []byte("lockdown-test-pass")

	v, err := Init(ctx, path, pw)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	v.Lockdown = true
	v.LockdownUnattendedOK = true
	if got := v.inferOnDiskVersion(); got != 2 {
		t.Errorf("lockdown policy should force v2, got v%d", got)
	}
	if err := v.Save(ctx, path, pw); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(ctx, path, pw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.Lockdown || !loaded.LockdownUnattendedOK {
		t.Errorf("lockdown policy lost across save/load: %+v", loaded)
	}
}
