package vault

import (
	"context"
	"fmt"
	"strings"

	"github.com/yemon/calypso/internal/crypto"
	"github.com/yemon/calypso/internal/project"
)

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
		defer crypto.Wipe(plain) // zero the marshalled plaintext after sealing
		blob, err := crypto.Encrypt(passphrase, plain)
		if err != nil {
			return err
		}
		if err := atomicWrite(path, blob); err != nil {
			return err
		}
		v.loadedVersion = 1
		return nil
	})
}
