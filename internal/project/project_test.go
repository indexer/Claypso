package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestSerializeEnv(t *testing.T) {
	vars := []Var{
		{Key: "DB_HOST", Value: "localhost"},
		{Key: "DB_PORT", Value: "5432"},
		{Key: "API_KEY", Value: "sk-abc123"},
	}
	out := SerializeEnv(vars)
	parsed := ParseEnv(out)
	if len(parsed) != len(vars) {
		t.Fatalf("roundtrip: expected %d, got %d", len(vars), len(parsed))
	}
	for i, v := range vars {
		if parsed[i].Key != v.Key {
			t.Errorf("key %d: expected %q, got %q", i, v.Key, parsed[i].Key)
		}
		if parsed[i].Value != v.Value {
			t.Errorf("value %d: expected %q, got %q", i, v.Value, parsed[i].Value)
		}
	}
}

func TestSerializeEnvNeedsQuoting(t *testing.T) {
	vars := []Var{
		{Key: "SPACED", Value: "hello world"},
		{Key: "HASHED", Value: "#comment"},
		{Key: "QUOTED", Value: `she said "hello"`},
	}
	out := SerializeEnv(vars)
	parsed := ParseEnv(out)

	vals := make(map[string]string)
	for _, v := range parsed {
		vals[v.Key] = v.Value
	}
	if vals["SPACED"] != "hello world" {
		t.Errorf("SPACED: expected 'hello world', got %q", vals["SPACED"])
	}
	if vals["HASHED"] != "#comment" {
		t.Errorf("HASHED: expected '#comment', got %q", vals["HASHED"])
	}
	if vals["QUOTED"] != `she said "hello"` {
		t.Errorf("QUOTED: unexpected value: %q", vals["QUOTED"])
	}
}

func TestGetSetUnset(t *testing.T) {
	p := &Project{Name: "test"}

	p.Set("KEY1", "val1")
	p.Set("KEY2", "val2")

	v, ok := p.Get("KEY1")
	if !ok || v != "val1" {
		t.Errorf("Get KEY1: expected val1, got %q (ok=%v)", v, ok)
	}

	_, ok = p.Get("NONEXISTENT")
	if ok {
		t.Error("Get NONEXISTENT should return false")
	}

	p.Set("KEY1", "updated")
	v, _ = p.Get("KEY1")
	if v != "updated" {
		t.Errorf("Set update: expected 'updated', got %q", v)
	}

	if !p.Unset("KEY1") {
		t.Error("Unset KEY1 should return true")
	}
	_, ok = p.Get("KEY1")
	if ok {
		t.Error("Get KEY1 after Unset should return false")
	}

	if p.Unset("NEVER_EXISTED") {
		t.Error("Unset NEVER_EXISTED should return false")
	}

	if len(p.Vars) != 1 {
		t.Errorf("expected 1 var remaining, got %d", len(p.Vars))
	}
}

func TestKeys(t *testing.T) {
	p := &Project{Name: "test"}
	p.Set("B", "2")
	p.Set("A", "1")
	p.Set("C", "3")

	keys := p.Keys()
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

func TestReadWriteEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")

	vars := []Var{
		{Key: "A", Value: "1"},
		{Key: "B", Value: "two"},
	}
	if err := WriteEnvFile(path, vars); err != nil {
		t.Fatalf("WriteEnvFile: %v", err)
	}

	read, err := ReadEnvFile(path)
	if err != nil {
		t.Fatalf("ReadEnvFile: %v", err)
	}

	if len(read) != 2 {
		t.Fatalf("expected 2 vars, got %d", len(read))
	}
	if read[0].Key != "A" || read[0].Value != "1" {
		t.Errorf("var[0]: expected A=1, got %s=%s", read[0].Key, read[0].Value)
	}
	if read[1].Key != "B" || read[1].Value != "two" {
		t.Errorf("var[1]: expected B=two, got %s=%s", read[1].Key, read[1].Value)
	}
}

func TestReadEnvFileNotExist(t *testing.T) {
	_, err := ReadEnvFile("/tmp/does/not/exist/.env")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestWriteEnvFilePerms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")

	if err := WriteEnvFile(path, nil); err != nil {
		t.Fatalf("WriteEnvFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("expected 0600 perms, got %#o", perm)
	}
}

func TestSerializeSafeEnv(t *testing.T) {
	vars := []Var{
		{Key: "DB_HOST", Value: "localhost"},
		{Key: "DB_PORT", Value: "5432"},
		{Key: "API_KEY", Value: "sk-secret-abc123"},
	}
	out := SerializeSafeEnv(vars)

	for _, v := range vars {
		if !strings.Contains(out, v.Key) {
			t.Errorf("output should contain key %q", v.Key)
		}
	}
	if strings.Contains(out, "localhost") || strings.Contains(out, "5432") || strings.Contains(out, "sk-secret") {
		t.Error("safe output must not contain real values")
	}
	if !strings.Contains(out, "****") {
		t.Error("safe output should use **** as placeholder")
	}

	parsed := ParseEnv(out)
	for _, v := range parsed {
		if v.Value != "****" {
			t.Errorf("key %q: expected ****, got %q", v.Key, v.Value)
		}
	}
}

func TestWriteSafeEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")

	vars := []Var{
		{Key: "SECRET", Value: "real-value"},
		{Key: "TOKEN", Value: "ghp_12345"},
	}
	if err := WriteSafeEnvFile(path, vars); err != nil {
		t.Fatalf("WriteSafeEnvFile: %v", err)
	}

	read, err := ReadEnvFile(path)
	if err != nil {
		t.Fatalf("ReadEnvFile: %v", err)
	}
	if len(read) != 2 {
		t.Fatalf("expected 2 vars, got %d", len(read))
	}
	for _, v := range read {
		if v.Value != "****" {
			t.Errorf("key %q: value should be ****, got %q", v.Key, v.Value)
		}
	}
}

func TestSerializeExampleEnv(t *testing.T) {
	vars := []Var{
		{Key: "DB_HOST", Value: "localhost"},
		{Key: "API_KEY", Value: "secret"},
	}
	out := SerializeExampleEnv(vars)

	for _, v := range vars {
		if !strings.Contains(out, v.Key) {
			t.Errorf("output should contain key %q", v.Key)
		}
	}
	if strings.Contains(out, "localhost") || strings.Contains(out, "secret") {
		t.Error("example output must not contain real values")
	}

	parsed := ParseEnv(out)
	for _, v := range parsed {
		if v.Value != "" {
			t.Errorf("key %q: expected empty value, got %q", v.Key, v.Value)
		}
	}
}

func TestWriteExampleEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")

	vars := []Var{
		{Key: "SECRET", Value: "real-val"},
		{Key: "TOKEN", Value: "ghp_12345"},
	}
	if err := WriteExampleEnvFile(path, vars); err != nil {
		t.Fatalf("WriteExampleEnvFile: %v", err)
	}

	read, err := ReadEnvFile(path)
	if err != nil {
		t.Fatalf("ReadEnvFile: %v", err)
	}
	if len(read) != 2 {
		t.Fatalf("expected 2 vars, got %d", len(read))
	}
	for _, v := range read {
		if v.Value != "" {
			t.Errorf("key %q: expected empty value, got %q", v.Key, v.Value)
		}
	}
}
