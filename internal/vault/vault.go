// Package vault is the encrypted store that holds every project's
// environments. The on-disk file is a single opaque encrypted blob; in
// memory it is a Vault struct. Nothing is ever written in plaintext.
package vault

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/yemon/calypso/internal/crypto"
	"github.com/yemon/calypso/internal/lockfile"
	"github.com/yemon/calypso/internal/project"
)

// Vault is the decrypted, in-memory representation.
//
// loadedVersion tracks the schema version observed on disk so writeUnlocked
// can hold the line: it never downgrades, and when an upgrade is unavoidable
// (e.g. a project gains a second env), the previous on-disk file is backed
// up to vault.enc.v<N>.bak before being overwritten.
type Vault struct {
	Version   int                         `json:"version"`
	Projects  map[string]*project.Project `json:"projects"`
	CreatedAt string                      `json:"created_at"`

	loadedVersion int            `json:"-"` // 0 for brand-new vaults; otherwise the version read from disk
	lastBackupErr error          `json:"-"` // set by writeUnlocked when auto-backup fails (non-fatal)
	cipher        *crypto.Cipher `json:"-"` // derived once on Load/Init, reused by writeUnlocked to skip a second Argon2id
	fingerprint   []byte         `json:"-"` // sha256 of the on-disk blob at load; nil for never-persisted vaults. Used for compare-and-swap on save.
}

// Close zeroes the cached derived key. Call it when the vault is no longer
// needed — most useful for a long-lived read-only holder like the dashboard,
// which decrypts once and never re-encrypts. CLI commands exit shortly after
// their final save, so the OS reclaims the memory regardless.
func (v *Vault) Close() {
	v.cipher.Clear()
}

// LastBackupErr returns the most recent auto-backup error, or nil. The save
// itself succeeded; this surfaces backup problems (disk full, perms) so the
// CLI can warn without failing the user's primary command.
func (v *Vault) LastBackupErr() error { return v.lastBackupErr }

var (
	ErrExists      = errors.New("vault already exists")
	ErrNotFound    = errors.New("vault not found; it is created automatically on first use, or run `calypso init`")
	ErrNoProject   = errors.New("no such project")
	ErrNoEnv       = errors.New("no such environment")
	ErrDuplicate   = errors.New("project already registered")
	ErrEnvRequired = errors.New("project has multiple environments; specify with name@env")
	ErrBadEnvName  = errors.New("invalid environment name")
	ErrConflict    = errors.New("vault changed on disk since it was read; re-run the command")
)

// blobFingerprint returns a content hash of an on-disk vault blob, used to
// detect whether the file changed between a Load and the matching Save.
func blobFingerprint(blob []byte) []byte {
	sum := sha256.Sum256(blob)
	return sum[:]
}

// checkNoConflict returns ErrConflict if the on-disk vault differs from what
// this Vault was loaded from — i.e. another process wrote it in the gap
// between our Load and this Save. The caller must hold the advisory lock so
// this read-and-compare is atomic with the write that follows. A nil
// fingerprint (a never-persisted vault, e.g. fresh Init) skips the check.
func (v *Vault) checkNoConflict(path string) error {
	if v.fingerprint == nil {
		return nil
	}
	cur, err := os.ReadFile(path)
	if err != nil {
		return nil // nothing on disk to clobber (or unreadable) — let the write proceed
	}
	if !bytes.Equal(blobFingerprint(cur), v.fingerprint) {
		return ErrConflict
	}
	return nil
}

// DefaultPath returns ~/.calypso/vault.enc
func DefaultPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".calypso", "vault.enc"), nil
}

func lockPath(vaultPath string) string {
	return vaultPath + ".lock"
}

func nowStamp() string { return time.Now().UTC().Format(time.RFC3339) }

// atomicWrite writes blob to path durably: it writes a sibling .tmp file with
// owner-only permissions and renames it into place, so a reader never observes
// a half-written vault. Every encrypted file the vault produces goes through
// here, keeping the write-then-rename contract in one place.
func atomicWrite(path string, blob []byte) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(blob); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	// Flush contents to stable storage before the rename, so a crash can't leave
	// the renamed-into-place vault with unwritten (zero/partial) data.
	if err := f.Sync(); err != nil {
		f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp) // don't leave a stale temp behind on failure
		return err
	}
	// Best-effort: fsync the directory so the rename entry itself is durable.
	// Directory fsync isn't supported everywhere (e.g. Windows), so ignore errors.
	if d, derr := os.Open(filepath.Dir(path)); derr == nil {
		_ = d.Sync() //nolint:errcheck // best-effort: directory fsync is unsupported on some platforms (e.g. Windows)
		_ = d.Close()
	}
	return nil
}

// withLock acquires the advisory lock on the vault and runs fn while it is held.
// The unlock (and underlying close) error is surfaced only when fn itself
// succeeded, so a release failure never masks the caller's primary error.
func withLock(vaultPath string, fn func() error) (err error) {
	f, lockErr := lockfile.Lock(lockPath(vaultPath))
	if lockErr != nil {
		return fmt.Errorf("vault locked by another process: %w", lockErr)
	}
	defer func() {
		if unlockErr := lockfile.Unlock(f); unlockErr != nil && err == nil {
			err = unlockErr
		}
	}()
	return fn()
}

// New creates an empty vault (in memory only).
func New() *Vault {
	return &Vault{
		Version:   schemaVersion,
		Projects:  make(map[string]*project.Project),
		CreatedAt: nowStamp(),
	}
}

// Init creates and persists a fresh vault. Fails if one already exists.
// Holds the lock from existence check through the first save to prevent races.
func Init(ctx context.Context, path string, passphrase []byte) (*Vault, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var v *Vault
	err := withLock(path, func() error {
		if _, err := os.Stat(path); err == nil {
			return ErrExists
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		v = New()
		return v.writeUnlocked(ctx, path, passphrase)
	})
	if err != nil {
		return nil, err
	}
	return v, nil
}

// Load decrypts the vault from disk, migrating older schemas as needed.
// Acquires an advisory lock to prevent dirty reads during a concurrent save.
func Load(ctx context.Context, path string, passphrase []byte) (*Vault, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var v *Vault
	err := withLock(path, func() error {
		blob, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return ErrNotFound
			}
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		plain, cipher, err := crypto.Open(passphrase, blob)
		if err != nil {
			return err
		}
		loaded, err := unmarshalAndMigrate(plain)
		// The decrypted JSON holds every secret; zero it now that values have
		// been copied into the Vault struct (the largest single plaintext copy).
		crypto.Wipe(plain)
		if err != nil {
			return err
		}
		// Reuse the salt+key from this decryption when the vault is saved
		// again in this process, so a load+save cycle derives the key once.
		loaded.cipher = cipher
		// Remember what was on disk so Save can detect a concurrent write
		// (compare-and-swap) instead of silently clobbering it.
		loaded.fingerprint = blobFingerprint(blob)
		v = loaded
		return nil
	})
	if err != nil {
		return nil, err
	}
	return v, nil
}

// Save encrypts and writes the vault atomically (write temp, then rename).
// Acquires an exclusive advisory lock to prevent concurrent writes.
func (v *Vault) Save(ctx context.Context, path string, passphrase []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return withLock(path, func() error {
		return v.writeUnlocked(ctx, path, passphrase)
	})
}

// Names returns project names sorted alphabetically.
func (v *Vault) Names() []string {
	return slices.Sorted(maps.Keys(v.Projects))
}

// Project fetches a project by name.
func (v *Vault) Project(name string) (*project.Project, error) {
	p, ok := v.Projects[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNoProject, name)
	}
	return p, nil
}
