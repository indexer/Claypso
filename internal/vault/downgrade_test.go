package vault

import (
	"os"
	"strings"
	"testing"
)

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

	post, _ := os.ReadFile(path)
	if string(post) != string(preBlob) {
		t.Error("file was modified despite refusal")
	}
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
