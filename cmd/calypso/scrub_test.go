package main

import (
	"bytes"
	"testing"

	"github.com/yemon/calypso/internal/project"
)

func scrubVars(pairs ...string) []project.Var {
	var out []project.Var
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, project.Var{Key: pairs[i], Value: project.SecretFromString(pairs[i+1])})
	}
	return out
}

// scrubAll runs input through a scrubWriter in chunks of chunkSize bytes
// (0 = one single write) and returns the fully flushed output.
func scrubAll(t *testing.T, vars []project.Var, input string, chunkSize int) string {
	t.Helper()
	var dst bytes.Buffer
	w := newScrubWriter(&dst, vars)
	data := []byte(input)
	if chunkSize <= 0 {
		chunkSize = len(data)
	}
	for len(data) > 0 {
		n := chunkSize
		if n > len(data) {
			n = len(data)
		}
		if _, err := w.Write(data[:n]); err != nil {
			t.Fatalf("Write: %v", err)
		}
		data = data[n:]
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return dst.String()
}

func TestScrubWriter_ReplacesValues(t *testing.T) {
	vars := scrubVars("API_KEY", "sk-test-abc123", "DB_PASS", "hunter2222")
	got := scrubAll(t, vars, "key=sk-test-abc123 pass=hunter2222 done\n", 0)
	want := "key=<concealed> pass=<concealed> done\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestScrubWriter_ValueSplitAcrossWrites(t *testing.T) {
	vars := scrubVars("SECRET", "supersecretvalue")
	for chunk := 1; chunk <= 7; chunk++ {
		got := scrubAll(t, vars, "before supersecretvalue after", chunk)
		want := "before <concealed> after"
		if got != want {
			t.Errorf("chunk=%d: got %q, want %q", chunk, got, want)
		}
	}
}

func TestScrubWriter_ShortValuesUntouched(t *testing.T) {
	// Values below minScrubLen must not be scrubbed — replacing "1" or "on"
	// would garble unrelated output.
	vars := scrubVars("DEBUG", "1", "MODE", "on", "REAL", "abcd")
	got := scrubAll(t, vars, "1 on abcd 1on", 0)
	want := "1 on <concealed> 1on"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestScrubWriter_LongestMatchWinsOnSharedPrefix(t *testing.T) {
	// LONG's value extends SHORT's; the longer one must be concealed as a
	// whole, not leak its suffix after a shorter-match replacement.
	vars := scrubVars("SHORT", "sk-live", "LONG", "sk-live-abcdef")
	got := scrubAll(t, vars, "x sk-live-abcdef y sk-live z", 0)
	want := "x <concealed> y <concealed> z"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestScrubWriter_RepeatedAndAdjacent(t *testing.T) {
	vars := scrubVars("K", "aaaabbbb")
	got := scrubAll(t, vars, "aaaabbbbaaaabbbb", 3)
	want := "<concealed><concealed>"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestScrubWriter_NoSecretsPassthrough(t *testing.T) {
	got := scrubAll(t, nil, "plain output, nothing to hide\n", 2)
	if got != "plain output, nothing to hide\n" {
		t.Errorf("passthrough mangled: %q", got)
	}
}

func TestScrubWriter_CloseFlushesTail(t *testing.T) {
	vars := scrubVars("SECRET", "supersecretvalue")
	var dst bytes.Buffer
	w := newScrubWriter(&dst, vars)
	// Partial prefix of the secret at end of stream: must be emitted verbatim
	// on Close, not swallowed.
	if _, err := w.Write([]byte("tail supersec")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if dst.String() == "tail supersec" {
		t.Fatalf("tail flushed before Close; hold-back not working")
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := dst.String(); got != "tail supersec" {
		t.Errorf("got %q, want %q", got, "tail supersec")
	}
}
