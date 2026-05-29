package project

import (
	"strings"
	"testing"
)

func TestParseEnvStripsBOM(t *testing.T) {
	// A UTF-8 BOM some editors prepend must not become part of the first key.
	vars := ParseEnv("\ufeffDB_HOST=localhost\nDB_PORT=5432")
	if len(vars) != 2 {
		t.Fatalf("expected 2 vars, got %d: %+v", len(vars), vars)
	}
	if vars[0].Key != "DB_HOST" {
		t.Errorf("first key should be DB_HOST without BOM, got %q", vars[0].Key)
	}
}

func TestParseEnvLongLineNotTruncated(t *testing.T) {
	// A value far larger than bufio.Scanner's default 64KB token cap (and the
	// old 1MB cap) must parse in full, and content after it must not be
	// silently dropped.
	big := strings.Repeat("x", 2*1024*1024) // 2 MB
	vars := ParseEnv("BIG=" + big + "\nAFTER=tail")
	if len(vars) != 2 {
		t.Fatalf("expected 2 vars; a long line must not drop the rest, got %d", len(vars))
	}
	got := map[string]string{}
	for _, v := range vars {
		got[v.Key] = v.Value.Reveal()
	}
	if len(got["BIG"]) != len(big) {
		t.Errorf("BIG truncated: got %d bytes, want %d", len(got["BIG"]), len(big))
	}
	if got["AFTER"] != "tail" {
		t.Errorf("content after a long line was dropped: AFTER=%q", got["AFTER"])
	}
}

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
		if checks[v.Key] != v.Value.Reveal() {
			t.Errorf("%s: expected %q, got %q", v.Key, checks[v.Key], v.Value.Reveal())
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
	if vars[0].Value.Reveal() != "localhost" {
		t.Errorf("DB_HOST: expected localhost, got %q", vars[0].Value.Reveal())
	}
}

func TestParseEnvDoubleQuotes(t *testing.T) {
	content := `DB_HOST="localhost"
API_KEY="sk-abc-123"`

	vars := ParseEnv(content)
	if len(vars) != 2 {
		t.Fatalf("expected 2 vars, got %d", len(vars))
	}
	if vars[0].Value.Reveal() != "localhost" {
		t.Errorf("expected localhost (unquoted), got %q", vars[0].Value.Reveal())
	}
}

func TestParseEnvSingleQuotes(t *testing.T) {
	content := `DB_HOST='localhost'
API_KEY='sk-abc-123'`

	vars := ParseEnv(content)
	if len(vars) != 2 {
		t.Fatalf("expected 2 vars, got %d", len(vars))
	}
	if vars[0].Value.Reveal() != "localhost" {
		t.Errorf("expected localhost (unquoted), got %q", vars[0].Value.Reveal())
	}
}

func TestParseEnvValueWithEquals(t *testing.T) {
	content := `JWT_SECRET=foo=bar=baz`
	vars := ParseEnv(content)
	if len(vars) != 1 {
		t.Fatalf("expected 1 var, got %d", len(vars))
	}
	if vars[0].Value.Reveal() != "foo=bar=baz" {
		t.Errorf("expected 'foo=bar=baz', got %q", vars[0].Value.Reveal())
	}
}

func TestParseEnvLeadingTrailingSpaces(t *testing.T) {
	content := `  DB_HOST =  localhost  
  API_KEY  =  "  sk-abc  "  `

	vars := ParseEnv(content)
	if len(vars) != 2 {
		t.Fatalf("expected 2 vars, got %d", len(vars))
	}
	if vars[0].Value.Reveal() != "localhost" {
		t.Errorf("DB_HOST: expected localhost, got %q", vars[0].Value.Reveal())
	}
	if vars[1].Value.Reveal() != "  sk-abc  " {
		t.Errorf("API_KEY: expected '  sk-abc  ', got %q", vars[1].Value.Reveal())
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
	if vars[0].Value.Reveal() != expectedCert {
		t.Errorf("CERT: expected %q, got %q", expectedCert, vars[0].Value.Reveal())
	}
	if vars[1].Value.Reveal() != "localhost" {
		t.Errorf("DB_HOST: expected localhost, got %q", vars[1].Value.Reveal())
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
	if vars[0].Value.Reveal() != "line one\nline two\nline three" {
		t.Errorf("TEXT: unexpected value: %q", vars[0].Value.Reveal())
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
	if vars[0].Value.Reveal() != expected {
		t.Errorf("escape sequences: expected %q, got %q", expected, vars[0].Value.Reveal())
	}
}
