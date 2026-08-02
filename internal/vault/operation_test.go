package vault

import (
	"strings"
	"testing"
)

func validTestOperation() *TrustedOperation {
	return &TrustedOperation{
		ID:             "op_0123456789abcdef",
		Label:          "staging health check",
		Spec:           "web",
		Method:         "GET",
		URL:            "http://127.0.0.1:8080/health",
		SecretKey:      "TOKEN",
		SecretHeader:   "Authorization",
		SecretPrefix:   "Bearer ",
		ExpectMin:      200,
		ExpectMax:      299,
		TimeoutSeconds: 10,
		CreatedAt:      "2026-01-01T00:00:00Z",
	}
}

func TestTrustedOperationPersistsAndForcesSchemaV3(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("operation-test-pass")
	v, err := Init(bg, path, pw)
	if err != nil {
		t.Fatal(err)
	}
	_, env, err := v.AddProject("web", "", "/tmp/web.env")
	if err != nil {
		t.Fatal(err)
	}
	env.Set("TOKEN", "runtime-secret")
	if err := v.AddTrustedOperation(validTestOperation()); err != nil {
		t.Fatal(err)
	}
	v.AgentStrict = true
	v.Lockdown = true
	if got := v.inferOnDiskVersion(); got != 3 {
		t.Fatalf("strict operation vault should require schema v3, got v%d", got)
	}
	if err := v.Save(bg, path, pw); err != nil {
		t.Fatal(err)
	}
	if got := onDiskVersion(t, path, pw); got != 3 {
		t.Fatalf("on-disk version = %d, want 3", got)
	}
	loaded, err := Load(bg, path, pw)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.AgentStrict || !loaded.Lockdown {
		t.Fatal("strict/lockdown state did not persist")
	}
	op, err := loaded.TrustedOperation(validTestOperation().ID)
	if err != nil {
		t.Fatal(err)
	}
	if op.SecretKey != "TOKEN" || op.URL != validTestOperation().URL {
		t.Fatal("trusted operation definition did not round-trip")
	}
}

func TestTrustedOperationValidation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*TrustedOperation)
	}{
		{name: "remote plaintext", mutate: func(op *TrustedOperation) { op.URL = "http://example.com/" }},
		{name: "credential in URL", mutate: func(op *TrustedOperation) { op.URL = "https://user:pass@example.com/" }},
		{name: "redirect fragment", mutate: func(op *TrustedOperation) { op.URL = "https://example.com/#secret" }},
		{name: "hop header", mutate: func(op *TrustedOperation) { op.SecretHeader = "Proxy-Authorization" }},
		{name: "label control", mutate: func(op *TrustedOperation) { op.Label = "bad\nlabel" }},
		{name: "prefix control", mutate: func(op *TrustedOperation) { op.SecretPrefix = "bad\r\n" }},
		{name: "bad method", mutate: func(op *TrustedOperation) { op.Method = "DELETE" }},
		{name: "bad status", mutate: func(op *TrustedOperation) { op.ExpectMin = 99 }},
		{name: "bad timeout", mutate: func(op *TrustedOperation) { op.TimeoutSeconds = 999 }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			op := validTestOperation()
			tc.mutate(op)
			if err := ValidateTrustedOperation(op); err == nil {
				t.Fatal("invalid operation was accepted")
			}
		})
	}
}

func TestTrustedOperationRequiresExistingCredential(t *testing.T) {
	v := New()
	if _, _, err := v.AddProject("web", "", "/tmp/web.env"); err != nil {
		t.Fatal(err)
	}
	err := v.AddTrustedOperation(validTestOperation())
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing credential should be rejected generically, got %v", err)
	}
}

func TestTrustedOperationBlocksV1Downgrade(t *testing.T) {
	v := New()
	_, env, _ := v.AddProject("web", "", "/tmp/web.env")
	env.Set("TOKEN", "runtime-secret")
	if err := v.AddTrustedOperation(validTestOperation()); err != nil {
		t.Fatal(err)
	}
	blockers := strings.Join(v.V1Blockers(), "\n")
	if !strings.Contains(blockers, "trusted operation") {
		t.Fatalf("downgrade blockers should preserve operations, got %s", blockers)
	}
}
