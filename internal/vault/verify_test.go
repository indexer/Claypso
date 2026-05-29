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

func TestVerifyAndRepair(t *testing.T) {
	v := New()
	v.CreatedAt = "" // fixable
	v.Projects["alpha"] = &project.Project{
		Name: "", // fixable: name doesn't match map key
		Envs: map[string]*project.Environment{
			"default": {Name: "", Path: "/abs/.env", UpdatedAt: ""},
		},
	}
	pre, fixes, post := VerifyAndRepair(bg, v)
	if len(pre) == 0 {
		t.Error("expected pre-repair findings")
	}
	if len(fixes) == 0 {
		t.Error("expected fixes to be applied")
	}
	for _, f := range post {
		if f.Severity == "error" {
			t.Errorf("error-severity finding remains after repair: %s", f.String())
		}
	}
}

func TestFindingString(t *testing.T) {
	f := Finding{Severity: "error", Where: "alpha@default", Message: "boom", Fixable: true}
	s := f.String()
	for _, want := range []string{"ERROR", "alpha@default", "boom", "[fixable]"} {
		if !strings.Contains(s, want) {
			t.Errorf("Finding.String()=%q should contain %q", s, want)
		}
	}
	if strings.Contains((Finding{Severity: "warning", Where: "x", Message: "m"}).String(), "[fixable]") {
		t.Error("a non-fixable finding must not be marked [fixable]")
	}
}

func TestRepair_DedupesDuplicateKeys(t *testing.T) {
	v := New()
	_, e, err := v.AddProject("alpha", "", "/tmp/alpha/.env")
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	e.Vars = []project.Var{
		{Key: "DB", Value: "old"},
		{Key: "API", Value: "x"},
		{Key: "DB", Value: "new"},
	}
	e.InvalidateIndex()

	// Verify flags the duplicate as a fixable error.
	foundDup := false
	for _, f := range Verify(v) {
		if strings.Contains(f.Message, "duplicate Var key") {
			foundDup = true
			if !f.Fixable {
				t.Error("duplicate-key finding should be Fixable")
			}
		}
	}
	if !foundDup {
		t.Fatal("Verify did not flag the duplicate key")
	}

	// Repair collapses to one entry, last value winning.
	Repair(v)
	if len(e.Vars) != 2 {
		t.Fatalf("expected 2 vars after repair, got %d: %+v", len(e.Vars), e.Vars)
	}
	if val, ok := e.Get("DB"); !ok || val != "new" {
		t.Errorf("DB after dedupe should be 'new', got %q ok=%v", val, ok)
	}
	for _, f := range Verify(v) {
		if strings.Contains(f.Message, "duplicate Var key") {
			t.Errorf("duplicate finding still present after repair: %v", f)
		}
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
