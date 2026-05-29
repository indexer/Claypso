// Package project models a registered project and its .env variables across
// one or more environments (dev, staging, production, etc.). The .env file
// parser and serializer live in parser.go and serialize.go.
package project

import (
	"fmt"
	"maps"
	"slices"
)

const SafePlaceholder = "****"

// DefaultEnvName is the env created when a project is added without an
// explicit env name. Loading a v1 vault also migrates each project's flat
// vars into an Environment under this name.
const DefaultEnvName = "default"

// Var is a single environment variable.
type Var struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ValidKey reports whether s is a usable env var name: a letter or underscore
// followed by letters, digits, or underscores. Enforcing this keeps generated
// .env files parseable and the keys safe to reference from a shell.
func ValidKey(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		isLetter := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_'
		isDigit := c >= '0' && c <= '9'
		if i == 0 && !isLetter {
			return false
		}
		if !isLetter && !isDigit {
			return false
		}
	}
	return true
}

// Environment is one named variant of a project (dev / staging / prod /
// default). It holds the .env path for that variant and its variables.
type Environment struct {
	Name      string `json:"name"`
	Path      string `json:"path"` // absolute
	Vars      []Var  `json:"vars"`
	UpdatedAt string `json:"updated_at"`

	keyIndex map[string]int `json:"-"` // lazy, key → index in Vars
}

// InvalidateIndex must be called by any caller that replaces or reorders
// e.Vars directly (e.g. push imports). Set/Unset keep the index in sync
// automatically.
func (e *Environment) InvalidateIndex() {
	e.keyIndex = nil
}

func (e *Environment) buildIndex() {
	if e.keyIndex != nil {
		return
	}
	e.keyIndex = make(map[string]int, len(e.Vars))
	for i, v := range e.Vars {
		e.keyIndex[v.Key] = i
	}
}

// Get returns the value for a key and whether it exists.
func (e *Environment) Get(key string) (string, bool) {
	e.buildIndex()
	if i, ok := e.keyIndex[key]; ok {
		return e.Vars[i].Value, true
	}
	return "", false
}

// Set inserts or updates a key, preserving insertion order for existing keys.
func (e *Environment) Set(key, value string) {
	e.buildIndex()
	if i, ok := e.keyIndex[key]; ok {
		e.Vars[i].Value = value
		return
	}
	e.keyIndex[key] = len(e.Vars)
	e.Vars = append(e.Vars, Var{Key: key, Value: value})
}

// Unset removes a key. Returns true if it existed.
func (e *Environment) Unset(key string) bool {
	e.buildIndex()
	i, ok := e.keyIndex[key]
	if !ok {
		return false
	}
	n := len(e.Vars) - 1
	delete(e.keyIndex, key)
	if i < n {
		e.Vars[i] = e.Vars[n]
		e.keyIndex[e.Vars[i].Key] = i
	}
	e.Vars = e.Vars[:n]
	return true
}

// Keys returns a sorted slice of the env's keys (for stable display).
func (e *Environment) Keys() []string {
	keys := make([]string, len(e.Vars))
	for i, v := range e.Vars {
		keys[i] = v.Key
	}
	slices.Sort(keys)
	return keys
}

// Project is a registered project with one or more named environments.
type Project struct {
	Name      string                  `json:"name"`
	Envs      map[string]*Environment `json:"envs"`
	UpdatedAt string                  `json:"updated_at"`
}

// Env fetches an environment by name.
func (p *Project) Env(name string) (*Environment, bool) {
	e, ok := p.Envs[name]
	return e, ok
}

// EnvNames returns environment names sorted alphabetically.
func (p *Project) EnvNames() []string {
	return slices.Sorted(maps.Keys(p.Envs))
}

// SoleEnv returns the project's only environment if it has exactly one.
// Used to resolve bare `myapp` references when no @env was supplied.
func (p *Project) SoleEnv() (*Environment, bool) {
	if len(p.Envs) != 1 {
		return nil, false
	}
	for _, e := range p.Envs {
		return e, true
	}
	return nil, false
}

// AddEnv attaches a new environment. Errors if one with that name already exists.
func (p *Project) AddEnv(name, absPath, stamp string) (*Environment, error) {
	if p.Envs == nil {
		p.Envs = make(map[string]*Environment)
	}
	if _, exists := p.Envs[name]; exists {
		return nil, fmt.Errorf("environment %q already exists for project %q", name, p.Name)
	}
	e := &Environment{
		Name:      name,
		Path:      absPath,
		UpdatedAt: stamp,
	}
	p.Envs[name] = e
	return e, nil
}

// RemoveEnv detaches an environment. Errors if it's the project's last env;
// callers should use Vault.RemoveProject in that case.
func (p *Project) RemoveEnv(name string) error {
	if _, ok := p.Envs[name]; !ok {
		return fmt.Errorf("environment %q not found in project %q", name, p.Name)
	}
	if len(p.Envs) == 1 {
		return fmt.Errorf("cannot remove last environment of project %q; remove the project instead", p.Name)
	}
	delete(p.Envs, name)
	return nil
}
