// Package analysis provides cross-project insights: key matrix, diff
// between two projects, and finding missing keys (gaps).
package analysis

import (
	"sort"
	"github.com/yemon/calypso/internal/project"
)

// VaultReader is the read-only surface this package needs.
type VaultReader interface {
	Names() []string
	Project(name string) (*project.Project, error)
}

// KeyUsage describes which projects define a given key.
type KeyUsage struct {
	Key      string
	Projects []string // sorted
}

// KeyMatrix returns every key and which projects define it.
func KeyMatrix(v VaultReader) []KeyUsage {
	seen := make(map[string][]string)
	for _, name := range v.Names() {
		p, err := v.Project(name)
		if err != nil {
			continue
		}
		for _, kv := range p.Vars {
			seen[kv.Key] = append(seen[kv.Key], name)
		}
	}
	out := make([]KeyUsage, 0, len(seen))
	for k, projs := range seen {
		sort.Strings(projs)
		out = append(out, KeyUsage{Key: k, Projects: projs})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// DiffEntry is one row of a two-project comparison.
type DiffEntry struct {
	Key    string
	ValueA string
	ValueB string
	InA    bool
	InB    bool
	Equal  bool
}

// Diff compares two projects key-by-key.
func Diff(v VaultReader, nameA, nameB string) ([]DiffEntry, error) {
	a, err := v.Project(nameA)
	if err != nil {
		return nil, err
	}
	b, err := v.Project(nameB)
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

	out := make([]DiffEntry, 0, len(keys))
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

// VarCounts is a summary of how two sets of vars compare.
type VarCounts struct {
	Added, Removed, Changed, Unchanged int
}

// DiffCounts compares two slices of vars by key.
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

// Gap is a missing-value finding: a project lacks a key that other projects define.
type Gap struct {
	Project   string
	Key       string
	DefinedIn []string
}

// FindGaps finds keys present in at least 2 projects that are missing from others.
func FindGaps(v VaultReader) []Gap {
	all := v.Names()
	if len(all) < 2 {
		return nil
	}

	keyProjects := make(map[string][]string)
	projectKeySet := make(map[string]map[string]bool, len(all))
	for _, name := range all {
		p, err := v.Project(name)
		if err != nil {
			continue
		}
		ks := make(map[string]bool, len(p.Vars))
		for _, kv := range p.Vars {
			ks[kv.Key] = true
			keyProjects[kv.Key] = append(keyProjects[kv.Key], name)
		}
		projectKeySet[name] = ks
	}

	var gaps []Gap
	// Pre-sort each key's project list once, not N times in the inner loop.
	for _, projs := range keyProjects {
		sort.Strings(projs)
	}
	for _, name := range all {
		myKeys := projectKeySet[name]
		for key, projs := range keyProjects {
			if len(projs) < 2 || myKeys[key] {
				continue
			}
			gaps = append(gaps, Gap{Project: name, Key: key, DefinedIn: projs})
		}
	}

	sort.Slice(gaps, func(i, j int) bool {
		if gaps[i].Project != gaps[j].Project {
			return gaps[i].Project < gaps[j].Project
		}
		return gaps[i].Key < gaps[j].Key
	})
	return gaps
}
