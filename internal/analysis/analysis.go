// Package analysis provides the cross-project insights that make the
// dashboard useful: which keys are shared, which are unique, and where a
// project is missing a key that its siblings (or its other envs) define.
package analysis

import (
	"sort"

	"github.com/yemon/calypso/internal/project"
)

// VaultReader is the narrow read-only surface this package needs. It is
// satisfied by *vault.Vault but stated here so callers and tests can pass
// any compatible implementation without importing the vault package.
type VaultReader interface {
	Names() []string
	Project(name string) (*project.Project, error)
	ResolveEnv(spec string) (*project.Project, *project.Environment, error)
}

// EnvRef identifies one environment for use in matrix/gap output.
type EnvRef struct {
	Project string
	Env     string
}

func (r EnvRef) String() string {
	if r.Env == "" || r.Env == project.DefaultEnvName {
		return r.Project
	}
	return r.Project + "@" + r.Env
}

// KeyUsage describes, for one key, which envs define it.
type KeyUsage struct {
	Key  string
	Refs []EnvRef // sorted
}

// KeyMatrix returns every key seen across all projects/envs and which envs
// define it.
func KeyMatrix(v VaultReader) []KeyUsage {
	seen := make(map[string][]EnvRef)
	for _, name := range v.Names() {
		p, err := v.Project(name)
		if err != nil {
			continue
		}
		for _, envName := range p.EnvNames() {
			e := p.Envs[envName]
			ref := EnvRef{Project: name, Env: envName}
			for _, kv := range e.Vars {
				seen[kv.Key] = append(seen[kv.Key], ref)
			}
		}
	}
	out := make([]KeyUsage, 0, len(seen))
	for k, refs := range seen {
		sort.Slice(refs, func(i, j int) bool {
			if refs[i].Project != refs[j].Project {
				return refs[i].Project < refs[j].Project
			}
			return refs[i].Env < refs[j].Env
		})
		out = append(out, KeyUsage{Key: k, Refs: refs})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// DiffEntry is one row of a two-env comparison.
type DiffEntry struct {
	Key    string
	ValueA string
	ValueB string
	InA    bool
	InB    bool
	Equal  bool
}

// Diff compares two envs key-by-key. Each spec is a `name[@env]` reference
// resolved through the vault — so `diff myapp@dev myapp@prod` and `diff
// alpha beta` both work.
func Diff(v VaultReader, specA, specB string) ([]DiffEntry, error) {
	_, a, err := v.ResolveEnv(specA)
	if err != nil {
		return nil, err
	}
	_, b, err := v.ResolveEnv(specB)
	if err != nil {
		return nil, err
	}

	keys := map[string]struct{}{}
	for _, kv := range a.Vars {
		keys[kv.Key] = struct{}{}
	}
	for _, kv := range b.Vars {
		keys[kv.Key] = struct{}{}
	}
	sorted := make([]string, 0, len(keys))
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)

	out := make([]DiffEntry, 0, len(sorted))
	for _, k := range sorted {
		va, inA := a.Get(k)
		vb, inB := b.Get(k)
		out = append(out, DiffEntry{
			Key: k, ValueA: va, ValueB: vb,
			InA: inA, InB: inB, Equal: inA && inB && va == vb,
		})
	}
	return out, nil
}

// VarCounts is a summary of how two []Var sets compare.
type VarCounts struct {
	Added, Removed, Changed, Unchanged int
}

// DiffCounts compares two flat slices of vars by key and returns a count
// summary. Used to show a compact "+a ~b -c =d" before destructive imports.
func DiffCounts(oldVars, newVars []project.Var) VarCounts {
	oldMap := make(map[string]string, len(oldVars))
	for _, v := range oldVars {
		oldMap[v.Key] = v.Value
	}
	newMap := make(map[string]string, len(newVars))
	for _, v := range newVars {
		newMap[v.Key] = v.Value
	}
	var c VarCounts
	for k, nv := range newMap {
		ov, ok := oldMap[k]
		switch {
		case !ok:
			c.Added++
		case ov != nv:
			c.Changed++
		default:
			c.Unchanged++
		}
	}
	for k := range oldMap {
		if _, ok := newMap[k]; !ok {
			c.Removed++
		}
	}
	return c
}

// Gap is a missing-value finding.
type Gap struct {
	Ref       EnvRef // the env that's missing the key
	Key       string
	DefinedIn []EnvRef // sibling envs that have it (sorted)
}

// FindCrossProjectGaps reports keys that exist in some projects' envs but
// are missing in another project's matching env. Treats each env as a peer
// of the same-named env in other projects.
//
// Only considers keys defined in at least 2 envs of that env-name across
// projects, to avoid flagging genuinely project-specific variables.
func FindCrossProjectGaps(v VaultReader) []Gap {
	all := v.Names()
	if len(all) < 2 {
		return nil
	}
	envKeys := make(map[EnvRef]map[string]bool)
	keyHavers := make(map[string]map[string][]EnvRef) // envName → key → refs

	for _, name := range all {
		p, err := v.Project(name)
		if err != nil {
			continue
		}
		for _, envName := range p.EnvNames() {
			e := p.Envs[envName]
			ref := EnvRef{Project: name, Env: envName}
			ks := make(map[string]bool, len(e.Vars))
			for _, kv := range e.Vars {
				ks[kv.Key] = true
				if keyHavers[envName] == nil {
					keyHavers[envName] = make(map[string][]EnvRef)
				}
				keyHavers[envName][kv.Key] = append(keyHavers[envName][kv.Key], ref)
			}
			envKeys[ref] = ks
		}
	}

	var gaps []Gap
	for ref, mine := range envKeys {
		peers, ok := keyHavers[ref.Env]
		if !ok {
			continue
		}
		for key, havers := range peers {
			if len(havers) < 2 {
				continue
			}
			if mine[key] {
				continue
			}
			sorted := append([]EnvRef(nil), havers...)
			sort.Slice(sorted, func(i, j int) bool { return sorted[i].Project < sorted[j].Project })
			gaps = append(gaps, Gap{Ref: ref, Key: key, DefinedIn: sorted})
		}
	}
	sortGaps(gaps)
	return gaps
}

// FindIntraProjectGaps reports keys that exist in some of a project's envs
// but are missing from others — common when staging gets a new key that
// nobody copied to production.
func FindIntraProjectGaps(v VaultReader) []Gap {
	var gaps []Gap
	for _, name := range v.Names() {
		p, err := v.Project(name)
		if err != nil {
			continue
		}
		if len(p.Envs) < 2 {
			continue
		}
		envNames := p.EnvNames()
		envSets := make(map[string]map[string]bool, len(envNames))
		keyEnvs := make(map[string][]EnvRef)
		for _, en := range envNames {
			e := p.Envs[en]
			ks := make(map[string]bool, len(e.Vars))
			ref := EnvRef{Project: name, Env: en}
			for _, kv := range e.Vars {
				ks[kv.Key] = true
				keyEnvs[kv.Key] = append(keyEnvs[kv.Key], ref)
			}
			envSets[en] = ks
		}
		for _, en := range envNames {
			mine := envSets[en]
			ref := EnvRef{Project: name, Env: en}
			for key, havers := range keyEnvs {
				if len(havers) == len(envNames) {
					continue
				}
				if mine[key] {
					continue
				}
				sorted := append([]EnvRef(nil), havers...)
				sort.Slice(sorted, func(i, j int) bool { return sorted[i].Env < sorted[j].Env })
				gaps = append(gaps, Gap{Ref: ref, Key: key, DefinedIn: sorted})
			}
		}
	}
	sortGaps(gaps)
	return gaps
}

func sortGaps(gaps []Gap) {
	sort.Slice(gaps, func(i, j int) bool {
		if gaps[i].Ref.Project != gaps[j].Ref.Project {
			return gaps[i].Ref.Project < gaps[j].Ref.Project
		}
		if gaps[i].Ref.Env != gaps[j].Ref.Env {
			return gaps[i].Ref.Env < gaps[j].Ref.Env
		}
		return gaps[i].Key < gaps[j].Key
	})
}
