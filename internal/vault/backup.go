package vault

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"
)

// (no helper needed beyond the standard library after the dedup removal)

// defaultBackupRetain is the number of timestamped backups kept by autoBackup
// when CALYPSO_BACKUP_RETAIN is unset or unparsable. Old enough to recover
// from a few accidental saves, small enough not to swamp the directory.
const defaultBackupRetain = 10

// backupTimeFormat is a compact, lexically-sortable UTC timestamp used in
// auto-backup filenames. Includes nanoseconds so rapid-fire saves don't
// collide on the same filename and silently overwrite each other.
// RFC3339 contains ':' and '+' which are awkward in filenames on some
// systems; this form sorts correctly as a plain string.
const backupTimeFormat = "20060102T150405.000000000Z"

// backupDir returns the sibling directory holding auto-backups for the
// given vault path: <vault>.backups/.
func backupDir(vaultPath string) string {
	return vaultPath + ".backups"
}

// autoBackup copies the post-save encrypted blob to <backupDir>/<ts>.enc
// and prunes older entries to retain at most retain copies.
//
// Note: we don't try to deduplicate identical saves. Each crypto.Encrypt
// call uses a fresh random salt and nonce, so two saves of the same
// plaintext produce different ciphertexts; byte-comparing encrypted
// blobs to skip "no-op" saves would never match. Plaintext-level dedup
// would be more useful in theory, but Touch updates the timestamp on
// every mutation so genuine no-op saves are rare in practice.
//
// Any failure is reported as a non-nil error but does not undo the save —
// callers should warn but not fail their primary operation, since the
// vault itself is intact at path.
func autoBackup(vaultPath string, blob []byte, retain int) error {
	if retain <= 0 {
		return nil
	}
	dir := backupDir(vaultPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("mkdir backup dir: %w", err)
	}
	name := time.Now().UTC().Format(backupTimeFormat) + ".enc"
	if err := os.WriteFile(filepath.Join(dir, name), blob, 0o600); err != nil {
		return fmt.Errorf("write backup: %w", err)
	}
	return pruneBackups(dir, retain)
}

// listBackups returns the sorted (oldest-first) names of auto-backup files
// in dir. Files that don't match the timestamp+extension naming are ignored
// so users (or other tooling) can drop unrelated files in the directory
// without affecting retention.
func listBackups(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if filepath.Ext(name) != ".enc" {
			continue
		}
		stem := name[:len(name)-len(".enc")]
		if _, err := time.Parse(backupTimeFormat, stem); err != nil {
			continue
		}
		out = append(out, name)
	}
	slices.Sort(out)
	return out, nil
}

// pruneBackups deletes the oldest entries until at most retain remain.
func pruneBackups(dir string, retain int) error {
	all, err := listBackups(dir)
	if err != nil {
		return err
	}
	if len(all) <= retain {
		return nil
	}
	for _, name := range all[:len(all)-retain] {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("prune backup %s: %w", name, err)
		}
	}
	return nil
}

// RetentionFromEnv reads CALYPSO_BACKUP_RETAIN and returns the parsed
// positive integer or defaultBackupRetain on any parse failure (including
// unset). A value of "0" disables auto-backup.
func RetentionFromEnv() int {
	v := os.Getenv("CALYPSO_BACKUP_RETAIN")
	if v == "" {
		return defaultBackupRetain
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return defaultBackupRetain
	}
	return n
}
