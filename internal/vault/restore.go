package vault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// BackupNames returns the auto-backup filenames (oldest first) for a vault
// at path. Returns an empty slice if the backup directory doesn't exist yet.
// No vault unlock is required — this only reads filesystem metadata.
func BackupNames(path string) ([]string, error) {
	return listBackups(backupDir(path))
}

// RestoreBackup replaces the vault file at path with the contents of the
// named backup. The current vault is first copied to <path>.pre-restore.bak
// so the user can undo an accidental restore. The named backup is left in
// place (not deleted). Acquires the vault lock for the duration.
//
// name is one of the entries returned by BackupNames — a bare filename
// like "20260524T093015Z.enc", not a path.
func RestoreBackup(ctx context.Context, path, name string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	src := filepath.Join(backupDir(path), name)
	if _, err := os.Stat(src); err != nil {
		return fmt.Errorf("backup %q not found: %w", name, err)
	}
	blob, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read backup: %w", err)
	}

	return withLock(path, func() error {
		// Preserve whatever is currently at path so the user can roll back
		// the restore itself. We don't try to be clever about format —
		// it's an opaque encrypted blob either way.
		if cur, err := os.ReadFile(path); err == nil {
			pre := path + ".pre-restore.bak"
			if err := os.WriteFile(pre, cur, 0o600); err != nil {
				return fmt.Errorf("snapshot current vault: %w", err)
			}
		}
		return atomicWrite(path, blob)
	})
}
