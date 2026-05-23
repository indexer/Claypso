package vault

import (
	"fmt"
	"path/filepath"

	"github.com/yemon/calypso/internal/project"
)

// AddProject registers a new project with an initial environment.
// envName may be empty, in which case project.DefaultEnvName is used.
func (v *Vault) AddProject(name, envName, envPath string) (*project.Project, *project.Environment, error) {
	if _, exists := v.Projects[name]; exists {
		return nil, nil, fmt.Errorf("%w: %q", ErrDuplicate, name)
	}
	if envName == "" {
		envName = project.DefaultEnvName
	}
	if err := ValidateEnvName(envName); err != nil {
		return nil, nil, err
	}
	abs, err := filepath.Abs(envPath)
	if err != nil {
		return nil, nil, err
	}
	stamp := nowStamp()
	p := &project.Project{
		Name:      name,
		Envs:      make(map[string]*project.Environment),
		UpdatedAt: stamp,
	}
	e, err := p.AddEnv(envName, abs, stamp)
	if err != nil {
		return nil, nil, err
	}
	v.Projects[name] = p
	return p, e, nil
}

// AddEnvToProject attaches a new env to an existing project.
func (v *Vault) AddEnvToProject(projectName, envName, envPath string) (*project.Environment, error) {
	p, err := v.Project(projectName)
	if err != nil {
		return nil, err
	}
	if err := ValidateEnvName(envName); err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(envPath)
	if err != nil {
		return nil, err
	}
	stamp := nowStamp()
	e, err := p.AddEnv(envName, abs, stamp)
	if err != nil {
		return nil, err
	}
	p.UpdatedAt = stamp
	return e, nil
}

// RemoveProject unregisters a project from the vault (does not touch disk).
func (v *Vault) RemoveProject(name string) error {
	if _, ok := v.Projects[name]; !ok {
		return fmt.Errorf("%w: %q", ErrNoProject, name)
	}
	delete(v.Projects, name)
	return nil
}

// RemoveEnv removes one env from a project. Errors if the env is the last one
// (callers should use RemoveProject instead).
func (v *Vault) RemoveEnv(projectName, envName string) error {
	p, err := v.Project(projectName)
	if err != nil {
		return err
	}
	if err := p.RemoveEnv(envName); err != nil {
		return err
	}
	p.UpdatedAt = nowStamp()
	return nil
}

// Touch updates a project's and resolved env's UpdatedAt timestamps. envName
// may be empty to touch only the project.
func (v *Vault) Touch(projectName, envName string) {
	p, ok := v.Projects[projectName]
	if !ok {
		return
	}
	stamp := nowStamp()
	p.UpdatedAt = stamp
	if envName == "" {
		return
	}
	if e, ok := p.Envs[envName]; ok {
		e.UpdatedAt = stamp
	}
}
