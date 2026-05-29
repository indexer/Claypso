package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

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

func TestSerializeEnvUnicodeRoundtrip(t *testing.T) {
	// Every value must survive SerializeEnv → ParseEnv unchanged. The
	// boundary-whitespace cases are regression guards: ParseEnv trims with the
	// Unicode-aware strings.TrimSpace, so values whose first/last rune is
	// non-ASCII whitespace (NBSP U+00A0, ideographic space U+3000) used to be
	// silently stripped because needsQuoting only checked ASCII whitespace.
	cases := []struct {
		name  string
		key   string
		value string
	}{
		{"cjk", "CJK", "日本語パスワード"},
		{"rtl_arabic", "RTL", "مرحبا-كلمة"},
		{"accents", "ACCENT", "café-naïve-Köln"},
		{"interior_emoji", "EMOJI", "pass🔑word"},
		{"emoji_zwj_sequence", "FAMILY", "👨‍👩‍👧‍👦"},
		{"emoji_only", "PARTY", "🎉"},
		{"interior_nbsp", "INNER_NBSP", "a\u00A0b"},
		{"leading_nbsp", "LEAD_NBSP", "\u00A0secret"},
		{"trailing_ideographic_space", "TRAIL_IDEO", "secret\u3000"},
		{"wrapping_nbsp", "WRAP_NBSP", "\u00A0value\u00A0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := SerializeEnv([]Var{{Key: tc.key, Value: tc.value}})
			parsed := ParseEnv(out)
			if len(parsed) != 1 {
				t.Fatalf("expected 1 var, got %d (serialized: %q)", len(parsed), out)
			}
			if parsed[0].Key != tc.key {
				t.Errorf("key: expected %q, got %q", tc.key, parsed[0].Key)
			}
			if parsed[0].Value != tc.value {
				t.Errorf("value roundtrip mismatch: expected %q, got %q (serialized: %q)", tc.value, parsed[0].Value, out)
			}
		})
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

func TestWriteEnvFileAtomicNoTemp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := WriteEnvFile(path, []Var{{Key: "A", Value: "1"}}); err != nil {
		t.Fatalf("WriteEnvFile: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file should not remain after an atomic write (stat err=%v)", err)
	}
	read, err := ReadEnvFile(path)
	if err != nil || len(read) != 1 || read[0].Value != "1" {
		t.Errorf("content after atomic write wrong: %+v (err=%v)", read, err)
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
