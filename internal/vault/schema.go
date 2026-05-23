package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/yemon/calypso/internal/crypto"
	"github.com/yemon/calypso/internal/project"
)

// schemaVersion bumps when the on-disk layout changes. Load handles older
// versions transparently via unmarshalAndMigrate / migrateV1.
const schemaVersion = 2

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
