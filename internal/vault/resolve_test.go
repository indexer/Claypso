package vault

import "testing"

func TestParseSpec(t *testing.T) {
	cases := []struct {
		in        string
		wantP     string
		wantEnv   string
		wantError bool
	}{
		{"myapp", "myapp", "", false},
		{"myapp@prod", "myapp", "prod", false},
		{"myapp@dev-1", "myapp", "dev-1", false},
		{"", "", "", true},
		{"@prod", "", "", true},
		{"myapp@", "", "", true},
		{"myapp@PROD", "", "", true}, // uppercase disallowed
		{"myapp@dev/staging", "", "", true},
	}
	for _, tc := range cases {
		s, err := ParseSpec(tc.in)
		if tc.wantError {
			if err == nil {
				t.Errorf("ParseSpec(%q): expected error, got %+v", tc.in, s)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseSpec(%q): unexpected error %v", tc.in, err)
			continue
		}
		if s.Project != tc.wantP || s.Env != tc.wantEnv {
			t.Errorf("ParseSpec(%q): got {%q,%q}, want {%q,%q}", tc.in, s.Project, s.Env, tc.wantP, tc.wantEnv)
		}
	}
}

func TestResolveEnvImplicitWhenSingle(t *testing.T) {
	v := New()
	v.AddProject("myapp", "dev", "/tmp/.env.dev")

	p, e, err := v.ResolveEnv("myapp")
	if err != nil {
		t.Fatalf("ResolveEnv with sole env should succeed: %v", err)
	}
	if p.Name != "myapp" || e.Name != "dev" {
		t.Errorf("got %s@%s, want myapp@dev", p.Name, e.Name)
	}
}

func TestResolveEnvRequiresExplicitWhenMultiple(t *testing.T) {
	v := New()
	v.AddProject("myapp", "dev", "/tmp/.env.dev")
	v.AddEnvToProject("myapp", "prod", "/tmp/.env.prod")

	if _, _, err := v.ResolveEnv("myapp"); err == nil {
		t.Error("ResolveEnv should require @env when multiple envs exist")
	}
	if _, e, err := v.ResolveEnv("myapp@prod"); err != nil || e.Name != "prod" {
		t.Errorf("explicit @prod failed: env=%+v err=%v", e, err)
	}
	if _, _, err := v.ResolveEnv("myapp@ghost"); err == nil {
		t.Error("ResolveEnv should error on unknown env")
	}
}
