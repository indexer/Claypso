package project

import "testing"

// FuzzParseEnv asserts ParseEnv never panics on arbitrary input — it does
// hand-rolled byte slicing across quotes, escapes, multiline values, and a BOM,
// so malformed input is the interesting case.
func FuzzParseEnv(f *testing.F) {
	for _, s := range []string{
		"KEY=value\n",
		"# comment\nexport A=b\n",
		"Q=\"quoted value\"\nS='single'\n",
		"M=\"line1\nline2\"\nN=after\n",
		"EQ=a=b=c\n",
		"\ufeffBOM=first\n",
		"ESC=\"a\\nb\\t\\\"c\"\n",
		"=noKey\nKEY=\n",
		"   spaced   =   trimmed   \n",
		"UNTERMINATED=\"oops\n",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, content string) {
		_ = ParseEnv(content) // must not panic
	})
}

// FuzzEnvRoundtrip asserts a single variable with a valid key and an arbitrary
// value survives SerializeEnv -> ParseEnv unchanged. A failure is a real
// data-loss bug on the pull -> push path.
func FuzzEnvRoundtrip(f *testing.F) {
	f.Add("KEY", "value")
	f.Add("A", "")
	f.Add("MULTI", "line1\nline2")
	f.Add("SPECIAL", "a=b #c \"q\" 't'\t")
	f.Add("UNI", "café   🔑")
	f.Add("CR", "a\rb")
	f.Fuzz(func(t *testing.T, key, value string) {
		if !ValidKey(key) {
			return // only valid keys are storable; key validity is tested elsewhere
		}
		out := SerializeEnv([]Var{{Key: key, Value: SecretFromString(value)}})
		parsed := ParseEnv(out)
		if len(parsed) != 1 {
			t.Fatalf("key %q: expected 1 var, got %d (serialized %q)", key, len(parsed), out)
		}
		if parsed[0].Key != key {
			t.Errorf("key mismatch: got %q want %q (serialized %q)", parsed[0].Key, key, out)
		}
		if got := parsed[0].Value.Reveal(); got != value {
			t.Errorf("value roundtrip mismatch: got %q want %q (serialized %q)", got, value, out)
		}
	})
}
