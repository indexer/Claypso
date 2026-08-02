package vault

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestLockdownPersistsAcrossSaveLoad(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "vault.enc")
	pw := []byte("lockdown-test-pass")

	v, err := Init(ctx, path, pw)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	v.Lockdown = true
	if err := v.Save(ctx, path, pw); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(ctx, path, pw)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.Lockdown {
		t.Fatal("Lockdown flag lost across save/load")
	}
}

func TestLockdownForcesSchemaV2(t *testing.T) {
	// A lockdown vault must never be written as v1 — that format has no
	// lockdown field and would silently drop the protection.
	v := New()
	if got := v.inferOnDiskVersion(); got != 1 {
		t.Fatalf("empty vault should infer v1, got v%d", got)
	}
	v.Lockdown = true
	if got := v.inferOnDiskVersion(); got != 2 {
		t.Fatalf("lockdown vault should infer v2, got v%d", got)
	}
}

func TestLockdownBlocksDowngrade(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "vault.enc")
	pw := []byte("lockdown-test-pass")

	v, err := Init(ctx, path, pw)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	v.Lockdown = true

	blockers := v.V1Blockers()
	found := false
	for _, b := range blockers {
		if strings.Contains(b, "lockdown") {
			found = true
		}
	}
	if !found {
		t.Fatalf("V1Blockers should mention lockdown, got %v", blockers)
	}
	if err := v.Downgrade(ctx, path, pw); err == nil {
		t.Fatal("Downgrade should fail while lockdown is on")
	}
}
