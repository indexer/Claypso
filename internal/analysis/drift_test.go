package analysis

import (
	"errors"
	"io/fs"
	"testing"

	"github.com/yemon/calypso/internal/project"
)

func TestDriftKindString(t *testing.T) {
	cases := map[DriftKind]string{
		DriftSame:        "same",
		DriftChanged:     "changed",
		DriftOnlyInVault: "only in vault",
		DriftOnlyOnDisk:  "only on disk",
		DriftKind(99):    "?",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("DriftKind(%d).String() = %q, want %q", int(k), got, want)
		}
	}
}

// withFakeDisk swaps in a fake disk reader for the duration of the test
// and restores the real one on cleanup. Lets us test drift without
// touching the filesystem.
func withFakeDisk(t *testing.T, vars []project.Var, err error) {
	t.Helper()
	orig := readDiskVars
	readDiskVars = func(string) ([]project.Var, error) {
		return vars, err
	}
	t.Cleanup(func() { readDiskVars = orig })
}

func env(name string, vars ...project.Var) *project.Environment {
	return &project.Environment{Name: name, Path: "/tmp/.env", Vars: vars}
}

func TestDrift_NoDifferences(t *testing.T) {
	vars := []project.Var{{Key: "A", Value: project.SecretFromString("1")}, {Key: "B", Value: project.SecretFromString("2")}}
	withFakeDisk(t, vars, nil)
	e := env("default", vars...)
	r, err := Drift(e, "myapp")
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}
	if r.HasDrift() {
		t.Errorf("expected no drift, got counts %+v", r.Counts)
	}
	if r.Counts.Unchanged != 2 {
		t.Errorf("expected 2 unchanged, got %d", r.Counts.Unchanged)
	}
}

func TestDrift_MissingDiskFile(t *testing.T) {
	withFakeDisk(t, nil, &fs.PathError{Op: "open", Err: fs.ErrNotExist})
	e := env("default", project.Var{Key: "A", Value: project.SecretFromString("1")})
	r, err := Drift(e, "myapp")
	if err != nil {
		t.Fatalf("Drift: %v", err)
	}
	if !r.Missing {
		t.Error("Missing should be true when disk file is absent")
	}
	if !r.HasDrift() {
		t.Error("missing disk file should count as drift")
	}
}

func TestDrift_PropagatesNonNotExistErrors(t *testing.T) {
	withFakeDisk(t, nil, errors.New("permission denied"))
	e := env("default", project.Var{Key: "A", Value: project.SecretFromString("1")})
	if _, err := Drift(e, "myapp"); err == nil {
		t.Error("non-NotExist disk errors should propagate, not be silently treated as Missing")
	}
}

func TestDriftDetails_Categorisation(t *testing.T) {
	disk := []project.Var{
		{Key: "A", Value: project.SecretFromString("1")},         // same
		{Key: "B", Value: project.SecretFromString("different")}, // changed
		{Key: "D", Value: project.SecretFromString("only-disk")}, // only on disk
	}
	withFakeDisk(t, disk, nil)
	e := env("default",
		project.Var{Key: "A", Value: project.SecretFromString("1")},
		project.Var{Key: "B", Value: project.SecretFromString("vault")},
		project.Var{Key: "C", Value: project.SecretFromString("only-vault")},
	)

	r, err := DriftDetails(e, "myapp")
	if err != nil {
		t.Fatalf("DriftDetails: %v", err)
	}
	if !r.HasDrift() {
		t.Error("expected drift to be reported")
	}
	kinds := make(map[string]DriftKind, len(r.Entries))
	for _, ent := range r.Entries {
		kinds[ent.Key] = ent.Kind
	}
	want := map[string]DriftKind{
		"A": DriftSame,
		"B": DriftChanged,
		"C": DriftOnlyInVault,
		"D": DriftOnlyOnDisk,
	}
	for k, w := range want {
		if got := kinds[k]; got != w {
			t.Errorf("%s: got kind %v, want %v", k, got, w)
		}
	}
}
