package vault

import (
	"fmt"
	"strings"

	"github.com/yemon/calypso/internal/project"
)

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
