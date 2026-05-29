package vault

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/yemon/calypso/internal/crypto"
	"github.com/yemon/calypso/internal/project"
)

// partialKind is the envelope discriminator written inside a partial-export
// blob. Today there's only "project" (one project + its envs), but the
// kind field is reserved so future export shapes can coexist with the same
// `import` command.
const partialKind = "project"

// partialEnvelope is the encrypted payload format for `vault export-project`.
// It's wrapped in the same crypto envelope as a full vault (Argon2id +
// secretbox) so the passphrase requirement is identical. The Kind
// discriminator lets Import auto-detect partial vs full blobs without
// any out-of-band signalling — we simply try v1/v2 parsing first and fall
// back to partial on failure.
type partialEnvelope struct {
	Kind       string           `json:"kind"`    // always partialKind today
	Project    *project.Project `json:"project"` // one project with one or more envs
	ExportedAt string           `json:"exported_at"`
}

// ExportProject writes a single project (one or more envs) to dst as an
// encrypted partial-export blob. passphrase is reused from the vault — the
// recipient needs the same passphrase to import.
//
// If spec includes @env, only that env is exported. Without @env the
// whole project (all envs) is exported.
func (v *Vault) ExportProject(ctx context.Context, spec, dst string, passphrase []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p, e, err := v.ResolveEnv(spec)
	if err != nil {
		// ResolveEnv requires explicit @env when multiple envs exist; for
		// export we want "whole project" to be valid too. Retry as a
		// project lookup if the spec is a bare name.
		s, perr := ParseSpec(spec)
		if perr != nil {
			return err
		}
		if s.Env != "" {
			return err // explicit @env that didn't resolve — surface the original error
		}
		whole, perr := v.Project(s.Project)
		if perr != nil {
			return perr
		}
		return writePartial(dst, passphrase, &partialEnvelope{
			Kind:       partialKind,
			Project:    cloneProject(whole),
			ExportedAt: nowStamp(),
		})
	}

	// Single-env export: clone the project but keep only the resolved env.
	sub := &project.Project{
		Name:      p.Name,
		Envs:      map[string]*project.Environment{e.Name: cloneEnv(e)},
		UpdatedAt: p.UpdatedAt,
	}
	return writePartial(dst, passphrase, &partialEnvelope{
		Kind:       partialKind,
		Project:    sub,
		ExportedAt: nowStamp(),
	})
}

// ImportProject reads a partial-export blob and merges its project into v.
// If the project name already exists, Import refuses unless force is true;
// with force, the existing project is replaced entirely (all envs).
//
// Returns the imported project name so the CLI can print useful output.
// Caller is responsible for Save.
func (v *Vault) ImportProject(passphrase []byte, src string, force bool) (string, error) {
	blob, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	plain, err := crypto.Decrypt(passphrase, blob)
	if err != nil {
		return "", err
	}
	var env partialEnvelope
	if err := json.Unmarshal(plain, &env); err != nil {
		return "", fmt.Errorf("not a recognised partial-export blob: %w", err)
	}
	if env.Kind != partialKind || env.Project == nil {
		return "", fmt.Errorf("not a partial-export blob (kind=%q)", env.Kind)
	}
	name := env.Project.Name
	if name == "" {
		return "", fmt.Errorf("partial-export blob has no project name")
	}
	if _, exists := v.Projects[name]; exists && !force {
		return name, fmt.Errorf("project %q already exists; pass --force to replace it", name)
	}
	if v.Projects == nil {
		v.Projects = make(map[string]*project.Project)
	}
	v.Projects[name] = env.Project
	return name, nil
}

// IsPartialExport peeks at a (decrypted) plaintext blob and reports
// whether it looks like a partial-export envelope. The caller decides
// what to do with that information; Import's auto-detection in the
// transfer command uses it to route between full-vault and partial paths.
func IsPartialExport(plain []byte) bool {
	var probe struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(plain, &probe); err != nil {
		return false
	}
	return probe.Kind == partialKind
}

func writePartial(dst string, passphrase []byte, env *partialEnvelope) error {
	plain, err := json.Marshal(env)
	if err != nil {
		return err
	}
	blob, err := crypto.Encrypt(passphrase, plain)
	if err != nil {
		return err
	}
	return atomicWrite(dst, blob)
}

// cloneProject does a deep enough copy that mutating the returned value
// won't disturb v. Vars slices are copied so the recipient's index can
// be invalidated independently.
func cloneProject(p *project.Project) *project.Project {
	out := &project.Project{
		Name:      p.Name,
		Envs:      make(map[string]*project.Environment, len(p.Envs)),
		UpdatedAt: p.UpdatedAt,
	}
	for k, e := range p.Envs {
		out.Envs[k] = cloneEnv(e)
	}
	return out
}

func cloneEnv(e *project.Environment) *project.Environment {
	out := &project.Environment{
		Name:      e.Name,
		Path:      e.Path,
		UpdatedAt: e.UpdatedAt,
		Vars:      make([]project.Var, len(e.Vars)),
	}
	copy(out.Vars, e.Vars)
	return out
}
