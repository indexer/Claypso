// Package vault is the encrypted store that holds every project's
// environments. The on-disk file is a single opaque encrypted blob; in
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
	"strings"
	"time"

	"github.com/yemon/calypso/internal/crypto"
	"github.com/yemon/calypso/internal/lockfile"
	"github.com/yemon/calypso/internal/project"
)

// schemaVersion bumps when the on-disk layout changes. Load handles older
// versions transparently via migrate().
const schemaVersion = 2

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

	loadedVersion int `json:"-"` // 0 for brand-new vaults; otherwise the version read from disk
}

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

// V1Blockers returns a list of human-readable reasons why this vault cannot
// be expressed in schema v1. An empty result means Downgrade would succeed.
// One entry per affected project so the caller can show all blockers at once.
func (v *Vault) V1Blockers() []string {
	var out []string
	for _, name := range v.Names() {
		p := v.Projects[name]
		if len(p.Envs) > 1 {
			out = append(out, fmt.Sprintf("%s: has %d envs (%s)", name, len(p.Envs), strings.Join(p.EnvNames(), ", ")))
			continue
		}
		if _, ok := p.Envs[project.DefaultEnvName]; !ok {
			for n := range p.Envs {
				out = append(out, fmt.Sprintf("%s: env named %q (v1 supports only %q)", name, n, project.DefaultEnvName))
				break
			}
		}
	}
	return out
}

// Downgrade rewrites the on-disk file in v1 format. It is the explicit
// escape hatch from the sticky-v2 rule in writeUnlocked. Fails if the
// vault has any project that isn't v1-representable (see V1Blockers). The
// current on-disk blob is backed up to <path>.v2.bak before being
// overwritten so the caller can roll back if needed.
func (v *Vault) Downgrade(ctx context.Context, path string, passphrase []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if blockers := v.V1Blockers(); len(blockers) > 0 {
		return fmt.Errorf("cannot downgrade to v1:\n  - %s", strings.Join(blockers, "\n  - "))
	}
	return withLock(path, func() error {
		if v.loadedVersion > 1 {
			if err := backupBeforeUpgrade(path, v.loadedVersion); err != nil {
				return fmt.Errorf("backup before downgrade: %w", err)
			}
		}
		plain, err := marshalForVersion(v, 1)
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
		if err := os.Rename(tmp, path); err != nil {
			return err
		}
		v.loadedVersion = 1
		return nil
	})
}

// writeUnlocked picks the minimum schema version that can represent the
// in-memory state ("hold-the-line"): single-env projects whose sole env is
// named project.DefaultEnvName fit v1, anything richer requires v2. Once a
// vault has been written as v2 the version is sticky — we never silently
// downgrade, even if the user later removes extra envs.
//
// When the on-disk version is about to change (e.g. an existing v1 file is
// being upgraded to v2 because a second env was added), the pre-upgrade
// blob is copied to vault.enc.v<N>.bak first so the user has a recovery
// path if they downgrade to an older binary.
func (v *Vault) writeUnlocked(ctx context.Context, path string, passphrase []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	target := v.inferOnDiskVersion()
	if v.loadedVersion > target {
		target = v.loadedVersion // sticky: don't auto-downgrade
	}

	if v.loadedVersion > 0 && target != v.loadedVersion {
		if err := backupBeforeUpgrade(path, v.loadedVersion); err != nil {
			return fmt.Errorf("backup before schema upgrade: %w", err)
		}
	}

	plain, err := marshalForVersion(v, target)
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
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	v.loadedVersion = target
	return nil
}

// inferOnDiskVersion returns the minimum schema version that can losslessly
// represent the current in-memory vault. A vault is v1-representable iff
// every project has exactly one env whose name is project.DefaultEnvName.
func (v *Vault) inferOnDiskVersion() int {
	for _, p := range v.Projects {
		if len(p.Envs) != 1 {
			return 2
		}
		if _, ok := p.Envs[project.DefaultEnvName]; !ok {
			return 2
		}
	}
	return 1
}

// backupBeforeUpgrade copies the existing on-disk blob to <path>.v<N>.bak
// when the next save will change the schema version. The blob stays
// encrypted (passphrase unchanged) so the backup is no less safe than the
// original. If a backup already exists from a prior attempt it is left
// alone — the first failure's backup is the most useful one.
func backupBeforeUpgrade(path string, sourceVersion int) error {
	src, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	bak := fmt.Sprintf("%s.v%d.bak", path, sourceVersion)
	if _, err := os.Stat(bak); err == nil {
		return nil
	}
	return os.WriteFile(bak, src, 0o600)
}

// AddProject registers a new project with an initial environment.
// envName may be empty, in which case project.DefaultEnvName is used.
func (v *Vault) AddProject(name, envName, envPath string) (*project.Project, *project.Environment, error) {
	if _, exists := v.Projects[name]; exists {
		return nil, nil, fmt.Errorf("%w: %q", ErrDuplicate, name)
	}
	if envName == "" {
		envName = project.DefaultEnvName
	}
	if err := ValidateEnvName(envName); err != nil {
		return nil, nil, err
	}
	abs, err := filepath.Abs(envPath)
	if err != nil {
		return nil, nil, err
	}
	stamp := nowStamp()
	p := &project.Project{
		Name:      name,
		Envs:      make(map[string]*project.Environment),
		UpdatedAt: stamp,
	}
	e, err := p.AddEnv(envName, abs, stamp)
	if err != nil {
		return nil, nil, err
	}
	v.Projects[name] = p
	return p, e, nil
}

// AddEnvToProject attaches a new env to an existing project.
func (v *Vault) AddEnvToProject(projectName, envName, envPath string) (*project.Environment, error) {
	p, err := v.Project(projectName)
	if err != nil {
		return nil, err
	}
	if err := ValidateEnvName(envName); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(envPath)
	if err != nil {
		return nil, err
	}
	stamp := nowStamp()
	e, err := p.AddEnv(envName, abs, stamp)
	if err != nil {
		return nil, err
	}
	p.UpdatedAt = stamp
	return e, nil
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

// RemoveEnv removes one env from a project. Errors if the env is the last one
// (callers should use RemoveProject instead).
func (v *Vault) RemoveEnv(projectName, envName string) error {
	p, err := v.Project(projectName)
	if err != nil {
		return err
	}
	if err := p.RemoveEnv(envName); err != nil {
		return err
	}
	p.UpdatedAt = nowStamp()
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

// Touch updates a project's and resolved env's UpdatedAt timestamps. envName
// may be empty to touch only the project.
func (v *Vault) Touch(projectName, envName string) {
	p, ok := v.Projects[projectName]
	if !ok {
		return
	}
	stamp := nowStamp()
	p.UpdatedAt = stamp
	if envName == "" {
		return
	}
	if e, ok := p.Envs[envName]; ok {
		e.UpdatedAt = stamp
	}
}

// Spec captures a parsed `name[@env]` reference.
type Spec struct {
	Project string
	Env     string // empty means "resolve to project's sole env"
}

// ParseSpec splits "name[@env]". The project name must be non-empty. When an
// env is present it is validated.
func ParseSpec(s string) (Spec, error) {
	at := strings.IndexByte(s, '@')
	if at < 0 {
		if s == "" {
			return Spec{}, fmt.Errorf("empty project name")
		}
		return Spec{Project: s}, nil
	}
	name := s[:at]
	env := s[at+1:]
	if name == "" {
		return Spec{}, fmt.Errorf("empty project name in %q", s)
	}
	if env == "" {
		return Spec{}, fmt.Errorf("empty env name in %q (use `name@env`)", s)
	}
	if err := ValidateEnvName(env); err != nil {
		return Spec{}, err
	}
	return Spec{Project: name, Env: env}, nil
}

// ResolveEnv parses `name[@env]` and returns the project and environment it
// refers to. If no env is given and the project has exactly one env, that
// env is returned; otherwise ErrEnvRequired is returned.
func (v *Vault) ResolveEnv(spec string) (*project.Project, *project.Environment, error) {
	s, err := ParseSpec(spec)
	if err != nil {
		return nil, nil, err
	}
	p, err := v.Project(s.Project)
	if err != nil {
		return nil, nil, err
	}
	if s.Env != "" {
		e, ok := p.Env(s.Env)
		if !ok {
			return nil, nil, fmt.Errorf("%w: %q in project %q (available: %s)",
				ErrNoEnv, s.Env, s.Project, strings.Join(p.EnvNames(), ", "))
		}
		return p, e, nil
	}
	e, ok := p.SoleEnv()
	if !ok {
		return nil, nil, fmt.Errorf("%w: %q has envs %s",
			ErrEnvRequired, s.Project, strings.Join(p.EnvNames(), ", "))
	}
	return p, e, nil
}

// ValidateEnvName enforces the allowed character set for env names. Keeps
// names safe in shell, URLs, filenames, and JSON map keys.
func ValidateEnvName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: empty", ErrBadEnvName)
	}
	if len(name) > 32 {
		return fmt.Errorf("%w: %q is longer than 32 chars", ErrBadEnvName, name)
	}
	for _, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_'
		if !ok {
			return fmt.Errorf("%w: %q (allowed: a-z, 0-9, -, _)", ErrBadEnvName, name)
		}
	}
	return nil
}

// v1Project / v1Vault are the on-disk shapes for schema version 1. Used by
// migrateV1 (read) and marshalForVersion (write back when v1-representable).
type v1Project struct {
	Name      string        `json:"name"`
	Path      string        `json:"path"`
	Vars      []project.Var `json:"vars"`
	UpdatedAt string        `json:"updated_at"`
}

type v1Vault struct {
	Version   int                   `json:"version"`
	Projects  map[string]*v1Project `json:"projects"`
	CreatedAt string                `json:"created_at"`
}

// unmarshalAndMigrate decodes a vault blob and brings it up to the in-memory
// representation, recording the source version in loadedVersion so writeUnlocked
// can later decide whether the on-disk format needs to change.
func unmarshalAndMigrate(plain []byte) (*Vault, error) {
	var head struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(plain, &head); err != nil {
		return nil, fmt.Errorf("vault decrypted but is unreadable: %w", err)
	}

	if head.Version < 2 {
		v, err := migrateV1(plain)
		if err != nil {
			return nil, err
		}
		v.loadedVersion = 1
		return v, nil
	}

	var v Vault
	if err := json.Unmarshal(plain, &v); err != nil {
		return nil, fmt.Errorf("vault decrypted but is unreadable: %w", err)
	}
	if v.Projects == nil {
		v.Projects = make(map[string]*project.Project)
	}
	v.loadedVersion = v.Version
	return &v, nil
}

// migrateV1 reads a v1-shaped blob and returns a v2 vault where each old
// project has a single Environment named project.DefaultEnvName.
func migrateV1(plain []byte) (*Vault, error) {
	var old v1Vault
	if err := json.Unmarshal(plain, &old); err != nil {
		return nil, fmt.Errorf("vault decrypted but is unreadable: %w", err)
	}
	out := &Vault{
		Version:   schemaVersion,
		Projects:  make(map[string]*project.Project, len(old.Projects)),
		CreatedAt: old.CreatedAt,
	}
	for name, op := range old.Projects {
		if op == nil {
			continue
		}
		e := &project.Environment{
			Name:      project.DefaultEnvName,
			Path:      op.Path,
			Vars:      op.Vars,
			UpdatedAt: op.UpdatedAt,
		}
		out.Projects[name] = &project.Project{
			Name:      name,
			Envs:      map[string]*project.Environment{project.DefaultEnvName: e},
			UpdatedAt: op.UpdatedAt,
		}
	}
	return out, nil
}

// marshalForVersion serializes v in the requested on-disk schema version.
// Callers must have already verified that v is representable at that version
// (see inferOnDiskVersion).
func marshalForVersion(v *Vault, version int) ([]byte, error) {
	switch version {
	case 1:
		out := v1Vault{
			Version:   1,
			Projects:  make(map[string]*v1Project, len(v.Projects)),
			CreatedAt: v.CreatedAt,
		}
		for name, p := range v.Projects {
			e := p.Envs[project.DefaultEnvName]
			if e == nil {
				return nil, fmt.Errorf("vault: cannot marshal project %q as v1 (default env missing)", name)
			}
			out.Projects[name] = &v1Project{
				Name:      p.Name,
				Path:      e.Path,
				Vars:      e.Vars,
				UpdatedAt: p.UpdatedAt,
			}
		}
		return json.Marshal(out)
	case 2:
		v.Version = 2
		return json.Marshal(v)
	default:
		return nil, fmt.Errorf("vault: unknown schema version %d", version)
	}
}
