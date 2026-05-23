package analysis

import (
	"testing"

	"github.com/yemon/calypso/internal/project"
	"github.com/yemon/calypso/internal/vault"
)

// makeVault builds the original 3-project, single-env fixture used for
// cross-project tests. Each project ends up with one env named "default".
func makeVault() *vault.Vault {
	v := vault.New()

	_, e1, _ := v.AddProject("alpha", "", "/a/.env")
	e1.Set("DB_HOST", "localhost")
	e1.Set("DB_PORT", "5432")
	e1.Set("API_KEY", "sk-alpha123")

	_, e2, _ := v.AddProject("beta", "", "/b/.env")
	e2.Set("DB_HOST", "localhost")
	e2.Set("DB_PORT", "3306")
	e2.Set("API_URL", "https://beta.example.com")

	_, e3, _ := v.AddProject("gamma", "", "/c/.env")
	e3.Set("DB_HOST", "localhost")
	e3.Set("DB_PORT", "5432")
	e3.Set("API_KEY", "sk-gamma456")
	e3.Set("GAMMA_SPECIFIC", "only-here")

	return v
}

func TestKeyMatrix(t *testing.T) {
	v := makeVault()
	matrix := KeyMatrix(v)

	if len(matrix) == 0 {
		t.Fatal("expected non-empty matrix")
	}

	keyMap := make(map[string][]EnvRef)
	for _, ku := range matrix {
		keyMap[ku.Key] = ku.Refs
	}

	if len(keyMap["DB_HOST"]) != 3 {
		t.Errorf("DB_HOST should be in 3 envs, got %d", len(keyMap["DB_HOST"]))
	}
	if len(keyMap["GAMMA_SPECIFIC"]) != 1 {
		t.Errorf("GAMMA_SPECIFIC should be in 1 env, got %d", len(keyMap["GAMMA_SPECIFIC"]))
	}

	for i := 1; i < len(matrix); i++ {
		if matrix[i-1].Key >= matrix[i].Key {
			t.Errorf("matrix not sorted: %s >= %s", matrix[i-1].Key, matrix[i].Key)
		}
	}
}

func TestDiff(t *testing.T) {
	v := makeVault()
	entries, err := Diff(v, "alpha", "beta")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	seen := make(map[string]DiffEntry)
	for _, e := range entries {
		seen[e.Key] = e
	}

	if !seen["DB_HOST"].Equal {
		t.Error("DB_HOST should be equal in alpha and beta")
	}
	if seen["DB_PORT"].Equal {
		t.Error("DB_PORT should differ between alpha and beta")
	}

	apiKey := seen["API_KEY"]
	if !apiKey.InA || apiKey.InB {
		t.Error("API_KEY should be in alpha only")
	}

	apiURL := seen["API_URL"]
	if apiURL.InA || !apiURL.InB {
		t.Error("API_URL should be in beta only")
	}
}

func TestDiffNotFound(t *testing.T) {
	v := makeVault()
	_, err := Diff(v, "alpha", "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent project")
	}
}

func TestDiffAcrossEnvsSameProject(t *testing.T) {
	v := vault.New()
	_, dev, _ := v.AddProject("myapp", "dev", "/tmp/.env.dev")
	prod, err := v.AddEnvToProject("myapp", "prod", "/tmp/.env.prod")
	if err != nil {
		t.Fatalf("AddEnvToProject: %v", err)
	}
	dev.Set("DB_HOST", "localhost")
	dev.Set("DEBUG", "1")
	prod.Set("DB_HOST", "db.prod.internal")
	prod.Set("CDN_URL", "https://cdn.example.com")

	entries, err := Diff(v, "myapp@dev", "myapp@prod")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	seen := map[string]DiffEntry{}
	for _, e := range entries {
		seen[e.Key] = e
	}
	if seen["DB_HOST"].Equal {
		t.Error("DB_HOST should differ between dev and prod")
	}
	if !seen["DEBUG"].InA || seen["DEBUG"].InB {
		t.Error("DEBUG should be in dev only")
	}
	if seen["CDN_URL"].InA || !seen["CDN_URL"].InB {
		t.Error("CDN_URL should be in prod only")
	}
}

func TestFindCrossProjectGaps(t *testing.T) {
	v := makeVault()
	gaps := FindCrossProjectGaps(v)

	gapMap := make(map[string]map[string]bool)
	for _, g := range gaps {
		ref := g.Ref.String()
		if gapMap[ref] == nil {
			gapMap[ref] = make(map[string]bool)
		}
		gapMap[ref][g.Key] = true
	}

	if !gapMap["beta"]["API_KEY"] {
		t.Error("beta should have gap: API_KEY (defined in alpha, gamma)")
	}
	if gapMap["alpha"]["API_URL"] {
		t.Error("API_URL only in beta (1 env), should not be flagged as a gap")
	}
	for _, g := range gaps {
		if g.Key == "GAMMA_SPECIFIC" {
			t.Errorf("GAMMA_SPECIFIC only in gamma, should not be flagged: ref=%s", g.Ref)
		}
		if g.Key == "DB_HOST" {
			t.Errorf("DB_HOST is in all envs, should not be a gap: ref=%s", g.Ref)
		}
	}
}

func TestFindIntraProjectGaps(t *testing.T) {
	v := vault.New()
	_, dev, _ := v.AddProject("myapp", "dev", "/tmp/.env.dev")
	staging, _ := v.AddEnvToProject("myapp", "staging", "/tmp/.env.staging")
	prod, _ := v.AddEnvToProject("myapp", "prod", "/tmp/.env.prod")

	dev.Set("DB_HOST", "localhost")
	dev.Set("STRIPE_KEY", "sk_test_123")
	staging.Set("DB_HOST", "staging.db")
	staging.Set("STRIPE_KEY", "sk_test_456")
	prod.Set("DB_HOST", "prod.db") // missing STRIPE_KEY → gap

	gaps := FindIntraProjectGaps(v)
	if len(gaps) == 0 {
		t.Fatal("expected at least one intra-project gap")
	}
	found := false
	for _, g := range gaps {
		if g.Ref.Project == "myapp" && g.Ref.Env == "prod" && g.Key == "STRIPE_KEY" {
			found = true
		}
	}
	if !found {
		t.Errorf("prod env should be flagged as missing STRIPE_KEY; got %+v", gaps)
	}
}

func TestDiffEmptyEnvs(t *testing.T) {
	v := vault.New()
	v.AddProject("a", "", "/a/.env")
	v.AddProject("b", "", "/b/.env")

	entries, err := Diff(v, "a", "b")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for empty envs, got %d", len(entries))
	}
}

// Compile-time check that *vault.Vault still satisfies VaultReader.
var _ VaultReader = (*vault.Vault)(nil)

// Silence unused-import warnings for project when the test stops needing it.
var _ = project.DefaultEnvName
