package vault

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	// name must be one of our own backups, never an arbitrary path. Enforcing
	// the documented "bare filename from BackupNames" contract via an allowlist
	// keeps a crafted name like "../../etc/x" from escaping the backup dir.
	known, err := BackupNames(path)
	if err != nil {
		return fmt.Errorf("list backups: %w", err)
	}
	if !slices.Contains(known, name) {
		return fmt.Errorf("no such backup %q (run `calypso vault backups` to list)", name)
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

// ImportRawBlob blind-copies an opaque encrypted blob over the vault at path,
// as a full-vault `import` does. It takes the vault lock and writes atomically
// (temp file + rename) so a concurrent calypso process can't interleave with
// the write and a crash mid-import can't leave a half-written vault — matching
// the durability contract every other vault write already follows. The blob is
// not validated here; the next command that opens the vault rejects it if the
// passphrase or format is wrong.
func ImportRawBlob(path string, blob []byte) error {
	return withLock(path, func() error {
		return atomicWrite(path, blob)
	})
}
