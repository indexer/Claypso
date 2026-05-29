package analysis

import (
	"cmp"
	"slices"
)

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
			sorted := slices.Clone(havers)
			slices.SortFunc(sorted, func(a, b EnvRef) int { return cmp.Compare(a.Project, b.Project) })
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
				sorted := slices.Clone(havers)
				slices.SortFunc(sorted, func(a, b EnvRef) int { return cmp.Compare(a.Env, b.Env) })
				gaps = append(gaps, Gap{Ref: ref, Key: key, DefinedIn: sorted})
			}
		}
	}
	sortGaps(gaps)
	return gaps
}

func sortGaps(gaps []Gap) {
	slices.SortFunc(gaps, func(a, b Gap) int {
		return cmp.Or(
			cmp.Compare(a.Ref.Project, b.Ref.Project),
			cmp.Compare(a.Ref.Env, b.Ref.Env),
			cmp.Compare(a.Key, b.Key),
		)
	})
}
