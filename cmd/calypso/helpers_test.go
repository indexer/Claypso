package main

import (
	"testing"

	"github.com/yemon/calypso/internal/project"
)

func TestMaybeMask(t *testing.T) {
	cases := []struct {
		val          string
		reveal, hint bool
		want         string
	}{
		{"sk-test-abc123", true, false, "sk-test-abc123"}, // reveal wins
		{"sk-test-abc123", true, true, "sk-test-abc123"},  // reveal beats hint
		{"sk-test-abc123", false, false, maskFixed},       // default: fixed width
		{"x", false, false, maskFixed},                    // short value: no leak
		{"", false, false, maskFixed},                     // empty: no leak
		{"sk-test-abc123", false, true, "sk**********23"}, // hint: edge chars
		{"abcd", false, true, "****"},                     // <=4 with hint
	}
	for _, c := range cases {
		got := maybeMask(c.val, c.reveal, c.hint)
		if got != c.want {
			t.Errorf("maybeMask(%q, reveal=%v, hint=%v) = %q, want %q", c.val, c.reveal, c.hint, got, c.want)
		}
	}
}

func TestMaybeMaskDefaultHidesLength(t *testing.T) {
	// Two values of different lengths must mask identically by default, so the
	// shareable view leaks neither length nor content.
	a := maybeMask("short", false, false)
	b := maybeMask("a-much-longer-secret-value", false, false)
	if a != b {
		t.Errorf("default mask leaks length: %q vs %q", a, b)
	}
}

func TestHasUnsyncedEdits(t *testing.T) {
	vault := []project.Var{{Key: "A", Value: "1"}, {Key: "B", Value: "2"}}

	tests := []struct {
		name string
		file []project.Var
		want bool
	}{
		{"identical", []project.Var{{Key: "A", Value: "1"}, {Key: "B", Value: "2"}}, false},
		{"fully masked (safe file)", []project.Var{{Key: "A", Value: project.SafePlaceholder}, {Key: "B", Value: project.SafePlaceholder}}, false},
		{"new real key on disk", []project.Var{{Key: "A", Value: "1"}, {Key: "C", Value: "new"}}, true},
		{"changed real value on disk", []project.Var{{Key: "A", Value: "changed"}}, true},
		{"key removed on disk only", []project.Var{{Key: "A", Value: "1"}}, false}, // pull re-adds B; not a lost edit
		{"empty file", nil, false},
	}
	for _, tc := range tests {
		if got := hasUnsyncedEdits(vault, tc.file); got != tc.want {
			t.Errorf("%s: hasUnsyncedEdits = %v, want %v", tc.name, got, tc.want)
		}
	}
}
