package project

import "testing"

func TestParseEnvBasic(t *testing.T) {
	content := `DB_HOST=localhost
DB_PORT=5432
API_KEY=sk-abc123`

	vars := ParseEnv(content)
	if len(vars) != 3 {
		t.Fatalf("expected 3 vars, got %d", len(vars))
	}

	checks := map[string]string{
		"DB_HOST": "localhost",
		"DB_PORT": "5432",
		"API_KEY": "sk-abc123",
	}
	for _, v := range vars {
		if checks[v.Key] != v.Value {
			t.Errorf("%s: expected %q, got %q", v.Key, checks[v.Key], v.Value)
		}
	}
}

func TestParseEnvCommentsAndBlank(t *testing.T) {
	content := `# This is a comment

DB_HOST=localhost
# Another comment
DB_PORT=5432

API_KEY=sk-abc123`

	vars := ParseEnv(content)
	if len(vars) != 3 {
		t.Fatalf("expected 3 vars, got %d", len(vars))
	}
}

func TestParseEnvExportPrefix(t *testing.T) {
	content := `export DB_HOST=localhost
export API_KEY=sk-abc123`

	vars := ParseEnv(content)
	if len(vars) != 2 {
		t.Fatalf("expected 2 vars, got %d", len(vars))
	}
	if vars[0].Value != "localhost" {
		t.Errorf("DB_HOST: expected localhost, got %q", vars[0].Value)
	}
}

func TestParseEnvDoubleQuotes(t *testing.T) {
	content := `DB_HOST="localhost"
API_KEY="sk-abc-123"`

	vars := ParseEnv(content)
	if len(vars) != 2 {
		t.Fatalf("expected 2 vars, got %d", len(vars))
	}
	if vars[0].Value != "localhost" {
		t.Errorf("expected localhost (unquoted), got %q", vars[0].Value)
	}
}

func TestParseEnvSingleQuotes(t *testing.T) {
	content := `DB_HOST='localhost'
API_KEY='sk-abc-123'`

	vars := ParseEnv(content)
	if len(vars) != 2 {
		t.Fatalf("expected 2 vars, got %d", len(vars))
	}
	if vars[0].Value != "localhost" {
		t.Errorf("expected localhost (unquoted), got %q", vars[0].Value)
	}
}

func TestParseEnvValueWithEquals(t *testing.T) {
	content := `JWT_SECRET=foo=bar=baz`
	vars := ParseEnv(content)
	if len(vars) != 1 {
		t.Fatalf("expected 1 var, got %d", len(vars))
	}
	if vars[0].Value != "foo=bar=baz" {
		t.Errorf("expected 'foo=bar=baz', got %q", vars[0].Value)
	}
}

func TestParseEnvLeadingTrailingSpaces(t *testing.T) {
	content := `  DB_HOST =  localhost  
  API_KEY  =  "  sk-abc  "  `

	vars := ParseEnv(content)
	if len(vars) != 2 {
		t.Fatalf("expected 2 vars, got %d", len(vars))
	}
	if vars[0].Value != "localhost" {
		t.Errorf("DB_HOST: expected localhost, got %q", vars[0].Value)
	}
	if vars[1].Value != "  sk-abc  " {
		t.Errorf("API_KEY: expected '  sk-abc  ', got %q", vars[1].Value)
	}
}

func TestParseEnvEmpty(t *testing.T) {
	vars := ParseEnv("")
	if len(vars) != 0 {
		t.Errorf("expected 0 vars, got %d", len(vars))
	}
}

func TestParseEnvOnlyComments(t *testing.T) {
	vars := ParseEnv("# just a comment\n# another one\n")
	if len(vars) != 0 {
		t.Errorf("expected 0 vars, got %d", len(vars))
	}
}

func TestParseEnvNoEquals(t *testing.T) {
	vars := ParseEnv("RANDOM_LINE\nANOTHER_LINE")
	if len(vars) != 0 {
		t.Errorf("expected 0 vars, got %d", len(vars))
	}
}

func TestParseEnvMultilineValue(t *testing.T) {
	content := `CERT="-----BEGIN CERT-----
line1
line2
-----END CERT-----"
DB_HOST=localhost`

	vars := ParseEnv(content)
	if len(vars) != 2 {
		t.Fatalf("expected 2 vars, got %d", len(vars))
	}
	if vars[0].Key != "CERT" {
		t.Errorf("first var should be CERT, got %q", vars[0].Key)
	}
	expectedCert := "-----BEGIN CERT-----\nline1\nline2\n-----END CERT-----"
	if vars[0].Value != expectedCert {
		t.Errorf("CERT: expected %q, got %q", expectedCert, vars[0].Value)
	}
	if vars[1].Value != "localhost" {
		t.Errorf("DB_HOST: expected localhost, got %q", vars[1].Value)
	}
}

func TestParseEnvMultilineSingleQuotes(t *testing.T) {
	content := `TEXT='line one
line two
line three'
AFTER=hello`

	vars := ParseEnv(content)
	if len(vars) != 2 {
		t.Fatalf("expected 2 vars, got %d", len(vars))
	}
	if vars[0].Key != "TEXT" {
		t.Errorf("first var should be TEXT, got %q", vars[0].Key)
	}
	if vars[0].Value != "line one\nline two\nline three" {
		t.Errorf("TEXT: unexpected value: %q", vars[0].Value)
	}
}

func TestParseEnvEscapeSequences(t *testing.T) {
	content := `VALUE="line1\nline2\ttabbed\\backslash\"quote\""
SIMPLE=ok`

	vars := ParseEnv(content)
	if len(vars) != 2 {
		t.Fatalf("expected 2 vars, got %d", len(vars))
	}
	expected := "line1\nline2\ttabbed\\backslash\"quote\""
	if vars[0].Value != expected {
		t.Errorf("escape sequences: expected %q, got %q", expected, vars[0].Value)
	}
}
