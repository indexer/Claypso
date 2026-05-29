package vault

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestAutoBackup_CreatedOnSave confirms a backup file appears the first
// time we save a vault, and that its content matches the encrypted blob
// the vault wrote to disk (a recovery would be a simple file copy).
func TestAutoBackup_CreatedOnSave(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	v, err := Init(bg, path, pw)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if v.LastBackupErr() != nil {
		t.Fatalf("first save backup err: %v", v.LastBackupErr())
	}
	names, err := BackupNames(path)
	if err != nil {
		t.Fatalf("BackupNames: %v", err)
	}
	if len(names) != 1 {
		t.Fatalf("expected 1 backup after Init, got %d (%v)", len(names), names)
	}
	main, _ := os.ReadFile(path)
	bak, _ := os.ReadFile(filepath.Join(backupDir(path), names[0]))
	if string(main) != string(bak) {
		t.Error("backup blob should match the vault file byte-for-byte")
	}
}

// TestAutoBackup_DistinctFilenameOnEachSave confirms that consecutive
// saves get distinct timestamped filenames (no silent overwrite). We
// intentionally do NOT dedup encrypted blobs — each Encrypt call uses a
// fresh salt+nonce, so byte-level dedup would never trigger anyway.
func TestAutoBackup_DistinctFilenameOnEachSave(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	v, _ := Init(bg, path, pw)
	if err := v.Save(bg, path, pw); err != nil {
		t.Fatal(err)
	}
	names, _ := BackupNames(path)
	if len(names) != 2 {
		t.Errorf("expected 2 backups (Init + Save), got %d (%v)", len(names), names)
	}
	// Filenames must differ — the nanosecond-precision timestamp guarantees
	// this even for rapid-fire saves.
	if len(names) >= 2 && names[0] == names[1] {
		t.Errorf("backup filenames collided: %v", names)
	}
}

// TestAutoBackup_RetentionFromEnv confirms CALYPSO_BACKUP_RETAIN trims to
// the configured count after multiple distinct saves.
func TestAutoBackup_RetentionFromEnv(t *testing.T) {
	t.Setenv("CALYPSO_BACKUP_RETAIN", "3")
	path := vaultFile(t)
	pw := []byte("pw")
	v, _ := Init(bg, path, pw)
	// Make 5 distinct saves by toggling a project name's UpdatedAt via Touch.
	for i := 0; i < 5; i++ {
		v.AddProject("p"+strconv.Itoa(i), "", "/tmp/.env"+strconv.Itoa(i))
		if err := v.Save(bg, path, pw); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	names, _ := BackupNames(path)
	if len(names) != 3 {
		t.Errorf("retention=3 should keep 3 backups, got %d (%v)", len(names), names)
	}
}

// TestAutoBackup_RetentionZeroDisables confirms CALYPSO_BACKUP_RETAIN=0 is
// a kill switch (no backup directory created at all).
func TestAutoBackup_RetentionZeroDisables(t *testing.T) {
	t.Setenv("CALYPSO_BACKUP_RETAIN", "0")
	path := vaultFile(t)
	pw := []byte("pw")
	if _, err := Init(bg, path, pw); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(backupDir(path)); err == nil {
		t.Error("backup dir should not exist when retention=0")
	}
}

// TestRestoreBackupRejectsTraversal confirms RestoreBackup only accepts a bare
// filename that is actually one of our backups — never a path that escapes the
// backup directory.
func TestRestoreBackupRejectsTraversal(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	if _, err := Init(bg, path, pw); err != nil { // creates one backup
		t.Fatalf("Init: %v", err)
	}
	for _, bad := range []string{"../../etc/passwd", "..", "sub/x.enc", "/abs/x.enc", "not-a-backup.enc"} {
		if err := RestoreBackup(bg, path, bad); err == nil {
			t.Errorf("RestoreBackup(%q) = nil, want error", bad)
		}
	}
}

// TestRestoreBackup confirms that restoring a named backup overwrites the
// vault file, snapshots the pre-restore state, and restores the exact
// content that was in that backup.
//
// Timeline of backups created here:
//
//	names[0]  Init        empty vault
//	names[1]  +alpha      alpha only
//	names[2]  +beta       alpha + beta
//
// Restoring names[1] should give a vault with alpha but not beta.
func TestRestoreBackup(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("pw")
	v, _ := Init(bg, path, pw)

	v.AddProject("alpha", "", "/tmp/.env.alpha")
	v.Save(bg, path, pw)
	v.AddProject("beta", "", "/tmp/.env.beta")
	v.Save(bg, path, pw)

	names, _ := BackupNames(path)
	if len(names) < 3 {
		t.Fatalf("need 3 backups (Init + 2 saves), got %d: %v", len(names), names)
	}
	afterAlpha := names[1]

	if err := RestoreBackup(bg, path, afterAlpha); err != nil {
		t.Fatalf("RestoreBackup: %v", err)
	}
	if _, err := os.Stat(path + ".pre-restore.bak"); err != nil {
		t.Errorf("pre-restore snapshot missing: %v", err)
	}

	restored, err := Load(bg, path, pw)
	if err != nil {
		t.Fatalf("Load after restore: %v", err)
	}
	if _, err := restored.Project("alpha"); err != nil {
		t.Errorf("alpha should be present in restored vault: %v", err)
	}
	if _, err := restored.Project("beta"); err == nil {
		t.Error("beta should NOT be present in restored vault (it came after this backup)")
	}
}
