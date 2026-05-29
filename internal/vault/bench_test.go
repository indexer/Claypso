package vault

import (
	"path/filepath"
	"testing"
)

// BenchmarkVaultSaveLoad measures a save (cipher reused, no re-derivation) plus
// a load (one Argon2id derivation, which dominates) of a small vault.
func BenchmarkVaultSaveLoad(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "vault.enc")
	pw := []byte("benchmark passphrase")
	v, err := Init(bg, path, pw)
	if err != nil {
		b.Fatalf("Init: %v", err)
	}
	if _, _, err := v.AddProject("app", "", "/tmp/app/.env"); err != nil {
		b.Fatalf("AddProject: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := v.Save(bg, path, pw); err != nil {
			b.Fatalf("Save: %v", err)
		}
		if _, err := Load(bg, path, pw); err != nil {
			b.Fatalf("Load: %v", err)
		}
	}
}
