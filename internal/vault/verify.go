package vault

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/yemon/calypso/internal/project"
)

// Finding is one issue surfaced by Verify. Severity helps the caller pick
// an exit code (error → non-zero) and the CLI to colour the output.
type Finding struct {
	Severity string // "error" | "warning"
	Where    string // e.g. "myapp@prod" or "vault"
	Message  string
	Fixable  bool // true if Repair can address this finding
}

// Verify runs read-only sanity checks across the loaded vault and returns
// every problem it finds. Empty result means the vault is healthy. Verify
// never mutates v; pair it with Repair for trivial fixes.
//
// Checks (high-level):
//   - Vault has Projects map (non-nil) and a CreatedAt timestamp.
//   - Schema version is recognised (≤ schemaVersion).
//   - Each project has a non-empty name matching its map key.
//   - Each project has at least one env, with no nil entries.
//   - Each env has a non-empty name, an absolute Path, and no duplicate Var keys.
//   - Env names pass ValidateEnvName.
//   - Hold-the-line invariant: if any project has a v2-only shape,
//     loadedVersion (if observed) should be ≥ 2.
func Verify(v *Vault) []Finding {
	var out []Finding

	if v.Projects == nil {
		out = append(out, Finding{
			Severity: "error", Where: "vault",
			Message: "Projects map is nil", Fixable: true,
		})
	}
	if v.CreatedAt == "" {
		out = append(out, Finding{
			Severity: "warning", Where: "vault",
			Message: "CreatedAt is empty", Fixable: true,
		})
	}
	if v.Version > schemaVersion {
		out = append(out, Finding{
			Severity: "error", Where: "vault",
			Message: fmt.Sprintf("on-disk Version %d > supported %d (binary too old?)", v.Version, schemaVersion),
		})
	}

	for _, name := range v.Names() {
		p := v.Projects[name]
		out = append(out, verifyProject(name, p)...)
	}

	// Hold-the-line cross-check: if the in-memory shape can only be v2 but
	// the file we loaded claimed v1, there's a state-tracking bug.
	if v.loadedVersion == 1 && v.inferOnDiskVersion() == 2 {
		out = append(out, Finding{
			Severity: "warning", Where: "vault",
			Message: "loadedVersion=1 but current state requires v2 (next save will upgrade)",
		})
	}

	slices.SortStableFunc(out, func(a, b Finding) int {
		return cmp.Or(
			cmp.Compare(a.Severity, b.Severity), // "error" before "warning"
			cmp.Compare(a.Where, b.Where),
			cmp.Compare(a.Message, b.Message),
		)
	})
	return out
}

func verifyProject(mapKey string, p *project.Project) []Finding {
	var out []Finding
	where := mapKey

	if p == nil {
		return []Finding{{Severity: "error", Where: where, Message: "nil project"}}
	}
	if p.Name == "" {
		out = append(out, Finding{Severity: "error", Where: where, Message: "Name is empty", Fixable: true})
	} else if p.Name != mapKey {
		out = append(out, Finding{
			Severity: "error", Where: where,
			Message: fmt.Sprintf("Name %q doesn't match map key %q", p.Name, mapKey),
			Fixable: true,
		})
	}
	if len(p.Envs) == 0 {
		out = append(out, Finding{Severity: "error", Where: where, Message: "project has no environments"})
		return out
	}
	if p.UpdatedAt == "" {
		out = append(out, Finding{Severity: "warning", Where: where, Message: "UpdatedAt is empty", Fixable: true})
	}

	for _, en := range p.EnvNames() {
		e := p.Envs[en]
		out = append(out, verifyEnv(mapKey, en, e)...)
	}
	return out
}

func verifyEnv(projectName, mapKey string, e *project.Environment) []Finding {
	var out []Finding
	where := projectName + "@" + mapKey

	if e == nil {
		return []Finding{{Severity: "error", Where: where, Message: "nil env"}}
	}
	if e.Name == "" {
		out = append(out, Finding{Severity: "error", Where: where, Message: "env Name is empty", Fixable: true})
	} else if e.Name != mapKey {
		out = append(out, Finding{
			Severity: "error", Where: where,
			Message: fmt.Sprintf("env Name %q doesn't match map key %q", e.Name, mapKey),
			Fixable: true,
		})
	}
	if err := ValidateEnvName(mapKey); err != nil {
		out = append(out, Finding{Severity: "error", Where: where, Message: err.Error()})
	}
	if e.Path == "" {
		out = append(out, Finding{Severity: "error", Where: where, Message: "env Path is empty"})
	} else if !filepath.IsAbs(e.Path) {
		out = append(out, Finding{
			Severity: "warning", Where: where,
			Message: fmt.Sprintf("Path %q is relative", e.Path), Fixable: true,
		})
	}
	if e.UpdatedAt == "" {
		out = append(out, Finding{Severity: "warning", Where: where, Message: "env UpdatedAt is empty", Fixable: true})
	}

	seen := make(map[string]bool, len(e.Vars))
	for _, kv := range e.Vars {
		if kv.Key == "" {
			out = append(out, Finding{Severity: "error", Where: where, Message: "empty Var key"})
			continue
		}
		if seen[kv.Key] {
			out = append(out, Finding{
				Severity: "error", Where: where,
				Message: fmt.Sprintf("duplicate Var key %q", kv.Key),
			})
		}
		seen[kv.Key] = true
	}
	return out
}

// Repair applies trivial fixes for the Fixable findings: rebuild missing
// timestamps, normalise relative paths to absolute, sync Name fields with
// map keys, ensure the Projects map is non-nil. Returns the list of fixes
// applied. Run Verify again after Repair to see what's left.
//
// Repair is in-memory only; the caller must Save to persist changes.
// Saving a repaired vault triggers the auto-backup so the pre-repair state
// is preserved.
func Repair(v *Vault) []string {
	var applied []string
	stamp := nowStamp()

	if v.Projects == nil {
		v.Projects = make(map[string]*project.Project)
		applied = append(applied, "vault: initialised empty Projects map")
	}
	if v.CreatedAt == "" {
		v.CreatedAt = stamp
		applied = append(applied, "vault: set CreatedAt")
	}

	for _, name := range v.Names() {
		p := v.Projects[name]
		if p == nil {
			continue // verify already reports this; Repair leaves the structural fix to the caller
		}
		if p.Name == "" || p.Name != name {
			old := p.Name
			p.Name = name
			applied = append(applied, fmt.Sprintf("%s: set Name (was %q)", name, old))
		}
		if p.UpdatedAt == "" {
			p.UpdatedAt = stamp
			applied = append(applied, fmt.Sprintf("%s: set UpdatedAt", name))
		}
		for _, en := range p.EnvNames() {
			e := p.Envs[en]
			if e == nil {
				continue
			}
			if e.Name == "" || e.Name != en {
				old := e.Name
				e.Name = en
				applied = append(applied, fmt.Sprintf("%s@%s: set env Name (was %q)", name, en, old))
			}
			if e.Path != "" && !filepath.IsAbs(e.Path) {
				abs, err := filepath.Abs(e.Path)
				if err == nil {
					old := e.Path
					e.Path = abs
					applied = append(applied, fmt.Sprintf("%s@%s: normalised Path (was %q)", name, en, old))
				}
			}
			if e.UpdatedAt == "" {
				e.UpdatedAt = stamp
				applied = append(applied, fmt.Sprintf("%s@%s: set env UpdatedAt", name, en))
			}
		}
	}
	return applied
}

// summary on one line for callers that want a quick health summary.
// Used by tests and the CLI banner.
func (f Finding) String() string {
	suf := ""
	if f.Fixable {
		suf = " [fixable]"
	}
	return fmt.Sprintf("[%s] %s: %s%s", strings.ToUpper(f.Severity), f.Where, f.Message, suf)
}

// VerifyAndRepair is a small convenience used by tests and the CLI
// fix-then-recheck flow. Returns (preFindings, fixes, postFindings).
func VerifyAndRepair(ctx context.Context, v *Vault) ([]Finding, []string, []Finding) {
	if err := ctx.Err(); err != nil {
		return nil, nil, []Finding{{Severity: "error", Where: "vault", Message: err.Error()}}
	}
	pre := Verify(v)
	fixes := Repair(v)
	post := Verify(v)
	return pre, fixes, post
}
