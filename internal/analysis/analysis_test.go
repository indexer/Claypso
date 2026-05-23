package analysis

import (
	"testing"

	"github.com/yemon/calypso/internal/project"
	"github.com/yemon/calypso/internal/vault"
)

func makeVault() *vault.Vault {
	v := vault.New()
	p1 := &project.Project{Name: "alpha", Path: "/a/.env"}
	p1.Set("DB_HOST", "localhost")
	p1.Set("DB_PORT", "5432")
	p1.Set("API_KEY", "sk-alpha123")

	p2 := &project.Project{Name: "beta", Path: "/b/.env"}
	p2.Set("DB_HOST", "localhost")
	p2.Set("DB_PORT", "3306")
	p2.Set("API_URL", "https://beta.example.com")

	p3 := &project.Project{Name: "gamma", Path: "/c/.env"}
	p3.Set("DB_HOST", "localhost")
	p3.Set("DB_PORT", "5432")
	p3.Set("API_KEY", "sk-gamma456")
	p3.Set("GAMMA_SPECIFIC", "only-here")

	v.Projects["alpha"] = p1
	v.Projects["beta"] = p2
	v.Projects["gamma"] = p3
	return v
}

func TestKeyMatrix(t *testing.T) {
	v := makeVault()
	matrix := KeyMatrix(v)

	if len(matrix) == 0 {
		t.Fatal("expected non-empty matrix")
	}

	keyMap := make(map[string][]string)
	for _, ku := range matrix {
		keyMap[ku.Key] = ku.Projects
	}

	if len(keyMap["DB_HOST"]) != 3 {
		t.Errorf("DB_HOST should be in 3 projects, got %d", len(keyMap["DB_HOST"]))
	}
	if len(keyMap["GAMMA_SPECIFIC"]) != 1 {
		t.Errorf("GAMMA_SPECIFIC should be in 1 project, got %d", len(keyMap["GAMMA_SPECIFIC"]))
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

func TestFindGaps(t *testing.T) {
	v := makeVault()
	gaps := FindGaps(v)

	gapMap := make(map[string]map[string]bool)
	for _, g := range gaps {
		if gapMap[g.Project] == nil {
			gapMap[g.Project] = make(map[string]bool)
		}
		gapMap[g.Project][g.Key] = true
	}

	if !gapMap["beta"]["API_KEY"] {
		t.Error("beta should have gap: API_KEY (defined in alpha, gamma)")
	}

	if gapMap["alpha"]["API_URL"] {
		t.Error("API_URL only in beta (1 project), should not be flagged as a gap")
	}

	for _, g := range gaps {
		if g.Key == "GAMMA_SPECIFIC" {
			t.Errorf("GAMMA_SPECIFIC only in gamma (1 project), should not be flagged: project=%s", g.Project)
		}
		if g.Key == "DB_HOST" {
			t.Errorf("DB_HOST is in all projects, should not be a gap: project=%s", g.Project)
		}
	}
}

func TestFindGapsSingleProjectKeysIgnored(t *testing.T) {
	v := makeVault()
	gaps := FindGaps(v)

	for _, g := range gaps {
		if g.Key == "GAMMA_SPECIFIC" && g.Project == "alpha" {
			// GAMMA_SPECIFIC is only in gamma (1 project), should be ignored
			t.Error("keys in only 1 project should not appear in gaps")
		}
	}
}

func TestDiffEmptyProjects(t *testing.T) {
	v := vault.New()
	v.Projects["a"] = &project.Project{Name: "a", Path: "/a/.env"}
	v.Projects["b"] = &project.Project{Name: "b", Path: "/b/.env"}

	entries, err := Diff(v, "a", "b")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for empty projects, got %d", len(entries))
	}
}
