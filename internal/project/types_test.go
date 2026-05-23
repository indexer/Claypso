package project

import "testing"

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
