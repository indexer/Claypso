package project

import (
	"slices"
	"strings"
	"testing"
)

func TestUnsetPreservesOrder(t *testing.T) {
	e := &Environment{Name: "default"}
	for _, k := range []string{"A", "B", "C", "D"} {
		e.Set(k, k)
	}
	if !e.Unset("B") {
		t.Fatal("Unset B should return true")
	}
	got := make([]string, len(e.Vars))
	for i, v := range e.Vars {
		got[i] = v.Key
	}
	if want := []string{"A", "C", "D"}; !slices.Equal(got, want) {
		t.Errorf("order after Unset: got %v, want %v", got, want)
	}
	// The lazily-rebuilt index must still resolve correctly after the delete.
	if v, ok := e.Get("D"); !ok || v != "D" {
		t.Errorf("Get(D) after Unset: got %q ok=%v", v, ok)
	}
}

func TestDedupeKeys(t *testing.T) {
	in := []Var{{Key: "A", Value: "1"}, {Key: "B", Value: "2"}, {Key: "A", Value: "3"}}
	out := DedupeKeys(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 vars, got %d: %+v", len(out), out)
	}
	// First-appearance position, last value wins.
	if out[0].Key != "A" || out[0].Value != "3" {
		t.Errorf("A should be at index 0 with last value 3, got %+v", out[0])
	}
	if out[1].Key != "B" || out[1].Value != "2" {
		t.Errorf("B should be at index 1 with value 2, got %+v", out[1])
	}
}

func TestValidateVars(t *testing.T) {
	good := []Var{{Key: "DB_HOST", Value: "x"}, {Key: "_K2", Value: "y"}}
	if err := ValidateVars(good); err != nil {
		t.Errorf("ValidateVars(good) = %v, want nil", err)
	}

	badKey := []Var{{Key: "OK", Value: "x"}, {Key: "BAD-KEY", Value: "y"}}
	err := ValidateVars(badKey)
	if err == nil {
		t.Fatal("ValidateVars(badKey) = nil, want error")
	}
	if !strings.Contains(err.Error(), "BAD-KEY") {
		t.Errorf("error should name the offending key, got %v", err)
	}

	// Over-length key and value are rejected.
	if ValidKey(strings.Repeat("K", MaxKeyLen+1)) {
		t.Error("ValidKey should reject a key longer than MaxKeyLen")
	}
	bigVal := []Var{{Key: "BIG", Value: strings.Repeat("v", MaxValueLen+1)}}
	if err := ValidateVars(bigVal); err == nil {
		t.Error("ValidateVars should reject a value larger than MaxValueLen")
	}
}

func TestValidKey(t *testing.T) {
	valid := []string{"KEY", "KEY1", "_KEY", "a", "API_KEY_2", "lower_case"}
	for _, k := range valid {
		if !ValidKey(k) {
			t.Errorf("ValidKey(%q) = false, want true", k)
		}
	}
	invalid := []string{"", "1KEY", "KEY-1", "FOO BAR", "KEY=", "a.b", " key", "ünïcode"}
	for _, k := range invalid {
		if ValidKey(k) {
			t.Errorf("ValidKey(%q) = true, want false", k)
		}
	}
}

func TestGetSetUnset(t *testing.T) {
	e := &Environment{Name: "default"}

	e.Set("KEY1", "val1")
	e.Set("KEY2", "val2")

	v, ok := e.Get("KEY1")
	if !ok || v != "val1" {
		t.Errorf("Get KEY1: expected val1, got %q (ok=%v)", v, ok)
	}

	_, ok = e.Get("NONEXISTENT")
	if ok {
		t.Error("Get NONEXISTENT should return false")
	}

	e.Set("KEY1", "updated")
	v, _ = e.Get("KEY1")
	if v != "updated" {
		t.Errorf("Set update: expected 'updated', got %q", v)
	}

	if !e.Unset("KEY1") {
		t.Error("Unset KEY1 should return true")
	}
	_, ok = e.Get("KEY1")
	if ok {
		t.Error("Get KEY1 after Unset should return false")
	}

	if e.Unset("NEVER_EXISTED") {
		t.Error("Unset NEVER_EXISTED should return false")
	}

	if len(e.Vars) != 1 {
		t.Errorf("expected 1 var remaining, got %d", len(e.Vars))
	}
}

func TestProjectEnvLifecycle(t *testing.T) {
	p := &Project{Name: "myapp", Envs: map[string]*Environment{}}

	if _, err := p.AddEnv("dev", "/tmp/.env", "now"); err != nil {
		t.Fatalf("AddEnv dev: %v", err)
	}
	if _, err := p.AddEnv("dev", "/tmp/other", "now"); err == nil {
		t.Error("AddEnv should reject duplicate env name")
	}
	if _, err := p.AddEnv("prod", "/tmp/.env.prod", "now"); err != nil {
		t.Fatalf("AddEnv prod: %v", err)
	}

	if _, ok := p.SoleEnv(); ok {
		t.Error("SoleEnv should return false when project has multiple envs")
	}
	names := p.EnvNames()
	if len(names) != 2 || names[0] != "dev" || names[1] != "prod" {
		t.Errorf("EnvNames sorted: got %v", names)
	}

	if err := p.RemoveEnv("dev"); err != nil {
		t.Fatalf("RemoveEnv dev: %v", err)
	}
	if _, ok := p.SoleEnv(); !ok {
		t.Error("SoleEnv should return true when one env remains")
	}
	if err := p.RemoveEnv("prod"); err == nil {
		t.Error("RemoveEnv should refuse to remove the last env")
	}
	if err := p.RemoveEnv("nonexistent"); err == nil {
		t.Error("RemoveEnv on unknown should error")
	}
}

func TestKeys(t *testing.T) {
	e := &Environment{Name: "default"}
	e.Set("B", "2")
	e.Set("A", "1")
	e.Set("C", "3")

	keys := e.Keys()
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
	expected := []string{"A", "B", "C"}
	for i, k := range keys {
		if k != expected[i] {
			t.Errorf("key[%d]: expected %q, got %q", i, expected[i], k)
		}
	}
}
