// Package vault is the encrypted store that holds every project's
// environment. The on-disk file is a single opaque encrypted blob; in
// memory it is a Vault struct. Nothing is ever written in plaintext.
package vault

import (
	"context"
	"encoding/json"
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

const schemaVersion = 1

// Vault is the decrypted, in-memory representation.
type Vault struct {
	Version   int                         `json:"version"`
	Projects  map[string]*project.Project `json:"projects"`
	CreatedAt string                      `json:"created_at"`
}

var (
	ErrExists    = errors.New("vault already exists")
	ErrNotFound  = errors.New("vault not found; run `calypso init` first")
	ErrNoProject = errors.New("no such project")
	ErrDuplicate = errors.New("project already registered")
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

// Load decrypts the vault from disk. Acquires an advisory lock to prevent
// dirty reads during a concurrent save.
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
		var loaded Vault
		if err := json.Unmarshal(plain, &loaded); err != nil {
			return fmt.Errorf("vault decrypted but is unreadable: %w", err)
		}
		if loaded.Projects == nil {
			loaded.Projects = make(map[string]*project.Project)
		}
		v = &loaded
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

// writeUnlocked is the internal variant used when the caller already holds the lock.
func (v *Vault) writeUnlocked(ctx context.Context, path string, passphrase []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	plain, err := json.Marshal(v)
	if err != nil {
		return err
	}
	blob, err := crypto.Encrypt(passphrase, plain)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, blob, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// AddProject registers a new project. Path should be the .env file path.
func (v *Vault) AddProject(name, envPath string) (*project.Project, error) {
	if _, exists := v.Projects[name]; exists {
		return nil, fmt.Errorf("%w: %q", ErrDuplicate, name)
	}
	abs, err := filepath.Abs(envPath)
	if err != nil {
		return nil, err
	}
	p := &project.Project{
		Name:      name,
		Path:      abs,
		Vars:      nil,
		UpdatedAt: nowStamp(),
	}
	v.Projects[name] = p
	return p, nil
}

// Project fetches a project by name.
func (v *Vault) Project(name string) (*project.Project, error) {
	p, ok := v.Projects[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNoProject, name)
	}
	return p, nil
}

// RemoveProject unregisters a project from the vault (does not touch disk).
func (v *Vault) RemoveProject(name string) error {
	if _, ok := v.Projects[name]; !ok {
		return fmt.Errorf("%w: %q", ErrNoProject, name)
	}
	delete(v.Projects, name)
	return nil
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

// Touch updates a project's UpdatedAt timestamp.
func (v *Vault) Touch(name string) {
	if p, ok := v.Projects[name]; ok {
		p.UpdatedAt = nowStamp()
	}
}
