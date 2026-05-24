package vault

import (
	"path/filepath"
	"testing"
)

func TestExportImportProject_SingleEnv(t *testing.T) {
	srcPath := vaultFile(t)
	pw := []byte("pw")
	src, _ := Init(bg, srcPath, pw)
	_, e, _ := src.AddProject("myapp", "prod", "/tmp/.env.prod")
	e.Set("DB_HOST", "db.prod.internal")
	src.Save(bg, srcPath, pw)

	// Export just the prod env to a sibling file.
	out := filepath.Join(t.TempDir(), "myapp-prod.cbk")
	if err := src.ExportProject(bg, "myapp@prod", out, pw); err != nil {
		t.Fatalf("ExportProject: %v", err)
	}

	// Import into a fresh vault.
	dstPath := vaultFile(t)
	dst, _ := Init(bg, dstPath, pw)
	name, err := dst.ImportProject(pw, out, false)
	if err != nil {
		t.Fatalf("ImportProject: %v", err)
	}
	if name != "myapp" {
		t.Errorf("imported name: got %q, want myapp", name)
	}
	_, ie, err := dst.ResolveEnv("myapp@prod")
	if err != nil {
		t.Fatalf("ResolveEnv after import: %v", err)
	}
	if v, ok := ie.Get("DB_HOST"); !ok || v != "db.prod.internal" {
		t.Errorf("imported env should preserve values: ok=%v v=%q", ok, v)
	}
}

func TestImportProject_RefusesConflictWithoutForce(t *testing.T) {
	srcPath := vaultFile(t)
	pw := []byte("pw")
	src, _ := Init(bg, srcPath, pw)
	src.AddProject("myapp", "", "/tmp/.env")
	src.Save(bg, srcPath, pw)

	out := filepath.Join(t.TempDir(), "p.cbk")
	src.ExportProject(bg, "myapp", out, pw)

	dstPath := vaultFile(t)
	dst, _ := Init(bg, dstPath, pw)
	dst.AddProject("myapp", "", "/other/.env") // conflict

	if _, err := dst.ImportProject(pw, out, false); err == nil {
		t.Error("ImportProject should refuse a name conflict without --force")
	}

	if _, err := dst.ImportProject(pw, out, true); err != nil {
		t.Errorf("ImportProject with --force should succeed, got: %v", err)
	}
	// After force-import, the env's Path should reflect the imported value.
	_, e, _ := dst.ResolveEnv("myapp")
	if e.Path != "/tmp/.env" {
		t.Errorf("after force-import, Path should be %q, got %q", "/tmp/.env", e.Path)
	}
}

func TestExportProject_WholeProject(t *testing.T) {
	srcPath := vaultFile(t)
	pw := []byte("pw")
	src, _ := Init(bg, srcPath, pw)
	src.AddProject("myapp", "dev", "/tmp/.env.dev")
	src.AddEnvToProject("myapp", "prod", "/tmp/.env.prod")
	src.Save(bg, srcPath, pw)

	out := filepath.Join(t.TempDir(), "myapp-all.cbk")
	if err := src.ExportProject(bg, "myapp", out, pw); err != nil {
		t.Fatalf("ExportProject whole project: %v", err)
	}

	dstPath := vaultFile(t)
	dst, _ := Init(bg, dstPath, pw)
	if _, err := dst.ImportProject(pw, out, false); err != nil {
		t.Fatalf("ImportProject: %v", err)
	}
	p, err := dst.Project("myapp")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Envs) != 2 {
		t.Errorf("whole-project export should bring both envs, got %d", len(p.Envs))
	}
}
