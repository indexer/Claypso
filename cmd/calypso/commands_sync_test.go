package main

import (
	"strings"
	"testing"
)

func TestBuildFlyPayload(t *testing.T) {
	vars := scrubVars("API_KEY", "sk-live-abc", "DB_HOST", "db.internal")
	got, err := buildFlyPayload(vars)
	if err != nil {
		t.Fatalf("buildFlyPayload: %v", err)
	}
	want := "API_KEY=sk-live-abc\nDB_HOST=db.internal\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestBuildFlyPayload_RejectsMultiline(t *testing.T) {
	vars := scrubVars("CERT", "line1\nline2", "OK_KEY", "value")
	if _, err := buildFlyPayload(vars); err == nil || !strings.Contains(err.Error(), "CERT") {
		t.Errorf("multiline value should be rejected naming the key, got %v", err)
	}
}

func TestK8sSecretName(t *testing.T) {
	tests := map[string]string{
		"myapp":       "myapp",
		"My_App":      "my-app",
		"api.service": "api-service",
		"-weird-":     "weird",
		"___":         "",
	}
	for in, want := range tests {
		if got := k8sSecretName(in); got != want {
			t.Errorf("k8sSecretName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildK8sSecretYAML(t *testing.T) {
	vars := scrubVars("API_KEY", "supersecretvalue")
	got := buildK8sSecretYAML("myapp", "prod", vars)
	if !strings.Contains(got, "name: myapp") || !strings.Contains(got, "namespace: prod") {
		t.Errorf("metadata missing: %s", got)
	}
	// base64("supersecretvalue")
	if !strings.Contains(got, "API_KEY: c3VwZXJzZWNyZXR2YWx1ZQ==") {
		t.Errorf("base64 data missing: %s", got)
	}
	if strings.Contains(got, "supersecretvalue") {
		t.Errorf("plaintext value leaked into manifest: %s", got)
	}
	// Multiline values must survive via base64 — no error path at all.
	multi := buildK8sSecretYAML("m", "", scrubVars("CERT", "a\nb"))
	if !strings.Contains(multi, "CERT: YQpi") {
		t.Errorf("multiline value should be base64-carried: %s", multi)
	}
}
