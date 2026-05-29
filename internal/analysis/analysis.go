// Package analysis provides the cross-project insights that make the
// dashboard useful: which keys are shared, which are unique, and where a
// project is missing a key that its siblings (or its other envs) define.
package analysis

import (
	"cmp"
	"maps"
	"slices"

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
		slices.SortFunc(refs, func(a, b EnvRef) int {
			return cmp.Or(cmp.Compare(a.Project, b.Project), cmp.Compare(a.Env, b.Env))
		})
		out = append(out, KeyUsage{Key: k, Refs: refs})
	}
	slices.SortFunc(out, func(a, b KeyUsage) int { return cmp.Compare(a.Key, b.Key) })
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
	sorted := slices.Sorted(maps.Keys(keys))

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
		oldMap[v.Key] = v.Value.Reveal()
	}
	newMap := make(map[string]string, len(newVars))
	for _, v := range newVars {
		newMap[v.Key] = v.Value.Reveal()
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
