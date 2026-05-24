// Package vault is the encrypted store that holds every project's
// environments. The on-disk file is a single opaque encrypted blob; in
// memory it is a Vault struct. Nothing is ever written in plaintext.
package vault

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

	loadedVersion int   `json:"-"` // 0 for brand-new vaults; otherwise the version read from disk
	lastBackupErr error `json:"-"` // set by writeUnlocked when auto-backup fails (non-fatal)
}

// LastBackupErr returns the most recent auto-backup error, or nil. The save
// itself succeeded; this surfaces backup problems (disk full, perms) so the
// CLI can warn without failing the user's primary command.
func (v *Vault) LastBackupErr() error { return v.lastBackupErr }

var (
	ErrExists      = errors.New("vault already exists")
	ErrNotFound    = errors.New("vault not found; run `calypso init` first")
	ErrNoProject   = errors.New("no such project")
	ErrNoEnv       = errors.New("no such environment")
	ErrDuplicate   = errors.New("project already registered")
	ErrEnvRequired = errors.New("project has multiple environments; specify with name@env")
	ErrBadEnvName  = errors.New("invalid environment name")
)

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

// withLock acquires the advisory lock on the vault and runs fn while it is held.
func withLock(vaultPath string, fn func() error) error {
	f, err := lockfile.Lock(lockPath(vaultPath))
	if err != nil {
		return fmt.Errorf("vault locked by another process: %w", err)
	}
	defer lockfile.Unlock(f)
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
		plain, err := crypto.Decrypt(passphrase, blob)
		if err != nil {
			return err
		}
		loaded, err := unmarshalAndMigrate(plain)
		if err != nil {
			return err
		}
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
	names := make([]string, 0, len(v.Projects))
	for n := range v.Projects {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Project fetches a project by name.
func (v *Vault) Project(name string) (*project.Project, error) {
	p, ok := v.Projects[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNoProject, name)
	}
	return p, nil
}
