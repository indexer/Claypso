package vault

import (
	"strings"
	"testing"

	"github.com/yemon/calypso/internal/project"
)

func TestVerify_HealthyVaultHasNoFindings(t *testing.T) {
	v := New()
	v.AddProject("alpha", "", "/tmp/alpha/.env")
	v.AddProject("beta", "dev", "/tmp/beta/.env.dev")
	v.AddEnvToProject("beta", "prod", "/tmp/beta/.env.prod")

	if got := Verify(v); len(got) != 0 {
		t.Errorf("expected no findings, got:\n%v", got)
	}
}

func TestVerify_FlagsAllKnownIssues(t *testing.T) {
	v := &Vault{} // nil Projects, empty CreatedAt
	got := Verify(v)
	hasMessage := func(sub string) bool {
		for _, f := range got {
			if strings.Contains(f.Message, sub) {
				return true
			}
		}
		return false
	}
	if !hasMessage("Projects map is nil") {
		t.Errorf("missing nil-Projects finding; got %v", got)
	}
	if !hasMessage("CreatedAt is empty") {
		t.Errorf("missing empty-CreatedAt finding; got %v", got)
	}
}

func TestVerify_DetectsNameKeyMismatch(t *testing.T) {
	v := New()
	v.Projects["alpha"] = &project.Project{
		Name: "WRONG", // map key says "alpha"
		Envs: map[string]*project.Environment{
			"default": {Name: "default", Path: "/abs/.env"},
		},
	}
	got := Verify(v)
	found := false
	for _, f := range got {
		if strings.Contains(f.Message, "doesn't match map key") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected name/key mismatch finding, got %v", got)
	}
}

func TestVerify_DetectsRelativePath(t *testing.T) {
	v := New()
	v.Projects["alpha"] = &project.Project{
		Name: "alpha",
		Envs: map[string]*project.Environment{
			"default": {Name: "default", Path: "relative/.env"},
		},
	}
	got := Verify(v)
	found := false
	for _, f := range got {
		if strings.Contains(f.Message, "is relative") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected relative-path warning, got %v", got)
	}
}

func TestVerify_DetectsDuplicateVarKeys(t *testing.T) {
	v := New()
	v.Projects["alpha"] = &project.Project{
		Name: "alpha",
		Envs: map[string]*project.Environment{
			"default": {
				Name: "default", Path: "/abs/.env",
				Vars: []project.Var{
					{Key: "A", Value: "1"},
					{Key: "A", Value: "2"},
				},
			},
		},
	}
	got := Verify(v)
	found := false
	for _, f := range got {
		if strings.Contains(f.Message, "duplicate Var key") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected duplicate-key finding, got %v", got)
	}
}

func TestRepair_FixesTrivialIssues(t *testing.T) {
	v := New()
	v.CreatedAt = ""
	v.Projects["alpha"] = &project.Project{
		Name: "", // mismatch with map key
		Envs: map[string]*project.Environment{
			"default": {Name: "", Path: "rel/.env", UpdatedAt: ""},
		},
	}
	fixes := Repair(v)
	if len(fixes) == 0 {
		t.Fatal("expected at least one fix")
	}

	// After repair, the obvious findings should be gone.
	post := Verify(v)
	for _, f := range post {
		if f.Severity == "error" {
			t.Errorf("error-severity finding remains after Repair: %s", f.String())
		}
	}
	if v.CreatedAt == "" {
		t.Error("CreatedAt should be set after repair")
	}
	if v.Projects["alpha"].Name != "alpha" {
		t.Errorf("Name should be repaired to map key, got %q", v.Projects["alpha"].Name)
	}
	if v.Projects["alpha"].Envs["default"].Name != "default" {
		t.Errorf("env Name should be repaired to map key")
	}
}
