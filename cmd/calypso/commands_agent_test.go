package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return m
}

func TestInitClaudeCode_FreshAndIdempotent(t *testing.T) {
	dir := t.TempDir()

	res, err := initClaudeCode(dir, false, false)
	if err != nil {
		t.Fatalf("initClaudeCode: %v", err)
	}
	if len(res.added) != len(claudeCodeDeny)+len(claudeCodeAllow) {
		t.Errorf("fresh run should add every rule, added %d", len(res.added))
	}

	cfg := readJSON(t, filepath.Join(dir, ".claude", "settings.json"))
	perms := cfg["permissions"].(map[string]any)
	deny := perms["deny"].([]any)
	found := false
	for _, d := range deny {
		if d == "Bash(CALYPSO_UNATTENDED=*)" {
			found = true
		}
	}
	if !found {
		t.Errorf("deny list missing the CALYPSO_UNATTENDED rule: %v", deny)
	}
	foundPullDeny, foundBrokerAllow := false, false
	for _, d := range deny {
		if d == "Bash(calypso pull:*)" {
			foundPullDeny = true
		}
	}
	for _, a := range perms["allow"].([]any) {
		if a == "Bash(calypso broker invoke:*)" {
			foundBrokerAllow = true
		}
	}
	if !foundPullDeny || !foundBrokerAllow {
		t.Errorf("strict broker deny/allow rules missing")
	}

	// Second run: everything skipped, nothing duplicated.
	res2, err := initClaudeCode(dir, false, false)
	if err != nil {
		t.Fatalf("second initClaudeCode: %v", err)
	}
	if len(res2.added) != 0 {
		t.Errorf("second run should add nothing, added %v", res2.added)
	}
	cfg2 := readJSON(t, filepath.Join(dir, ".claude", "settings.json"))
	deny2 := cfg2["permissions"].(map[string]any)["deny"].([]any)
	if len(deny2) != len(deny) {
		t.Errorf("second run duplicated rules: %d vs %d", len(deny2), len(deny))
	}
}

func TestInitClaudeCode_PreservesExistingSettings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	seed := `{"model":"opus","permissions":{"deny":["Bash(rm -rf:*)"],"allow":["Bash(go test:*)"]}}`
	if err := os.WriteFile(path, []byte(seed), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := initClaudeCode(dir, false, false); err != nil {
		t.Fatalf("initClaudeCode: %v", err)
	}
	cfg := readJSON(t, path)
	if cfg["model"] != "opus" {
		t.Errorf("unrelated key clobbered: %v", cfg["model"])
	}
	deny := cfg["permissions"].(map[string]any)["deny"].([]any)
	if deny[0] != "Bash(rm -rf:*)" {
		t.Errorf("existing deny rule lost or reordered: %v", deny)
	}
}

func TestInitClaudeCode_RefusesInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := initClaudeCode(dir, false, false); err == nil {
		t.Fatal("should refuse to merge into invalid JSON")
	}
}

func TestInitClaudeCode_DryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	res, err := initClaudeCode(dir, false, true)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if res.preview == "" {
		t.Error("dry-run should produce a preview")
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Error("dry-run must not create the settings file")
	}
}

func TestInitOpenCode_WrapsBareVerdictString(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(path, []byte(`{"permission":{"bash":"allow"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := initOpenCode(dir, false, false); err != nil {
		t.Fatalf("initOpenCode: %v", err)
	}
	cfg := readJSON(t, path)
	bash := cfg["permission"].(map[string]any)["bash"].(map[string]any)
	if bash["*"] != "allow" {
		t.Errorf("old bare verdict should survive as \"*\": %v", bash)
	}
	if bash["calypso*--reveal*"] != "deny" {
		t.Errorf("reveal glob missing: %v", bash)
	}
	if bash["secret-tool lookup*"] != "deny" {
		t.Errorf("keychain deny missing: %v", bash)
	}
	if bash["calypso pull*"] != "deny" || bash["calypso strict off*"] != "deny" {
		t.Errorf("strict-mode denies missing: %v", bash)
	}
}

func TestInitOpenCode_FreshFileGetsSchema(t *testing.T) {
	dir := t.TempDir()
	if _, err := initOpenCode(dir, false, false); err != nil {
		t.Fatalf("initOpenCode: %v", err)
	}
	cfg := readJSON(t, filepath.Join(dir, "opencode.json"))
	if cfg["$schema"] != "https://opencode.ai/config.json" {
		t.Errorf("fresh config should carry $schema, got %v", cfg["$schema"])
	}
}

func TestInitCodex_AppendAndIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(path, []byte("# My project\n\nExisting instructions.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := initCodex(dir, false)
	if err != nil {
		t.Fatalf("initCodex: %v", err)
	}
	if len(res.added) != 1 {
		t.Errorf("first run should add the section, got %v", res.added)
	}
	data, _ := os.ReadFile(path)
	content := string(data)
	if !strings.HasPrefix(content, "# My project") {
		t.Error("existing content must stay first")
	}
	if !strings.Contains(content, codexPolicyStart) || !strings.Contains(content, "CALYPSO_UNATTENDED") {
		t.Errorf("policy section missing: %s", content)
	}
	if !strings.Contains(content, "broker invoke") || strings.Contains(content, "pull <project> --inject") {
		t.Errorf("Codex policy must require broker-only credential use: %s", content)
	}

	// Second run: no change, no duplicate section.
	res2, err := initCodex(dir, false)
	if err != nil {
		t.Fatalf("second initCodex: %v", err)
	}
	if len(res2.added) != 0 {
		t.Errorf("second run should skip, got %v", res2.added)
	}
	data2, _ := os.ReadFile(path)
	if strings.Count(string(data2), codexPolicyStart) != 1 {
		t.Error("policy section duplicated")
	}
}

func TestUpsertMarkedSection_ReplacesStaleBlock(t *testing.T) {
	stale := "before\n" + codexPolicyStart + "\nold rules\n" + codexPolicyEnd + "\nafter\n"
	out, changed, err := upsertMarkedSection(stale, codexPolicyStart, codexPolicyEnd, codexPolicySection)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if !changed {
		t.Error("stale block should count as changed")
	}
	if strings.Contains(out, "old rules") {
		t.Error("stale content survived")
	}
	if !strings.HasPrefix(out, "before\n") || !strings.HasSuffix(out, "after\n") {
		t.Errorf("surrounding content damaged: %q", out)
	}
	if strings.Count(out, codexPolicyStart) != 1 {
		t.Error("marker duplicated")
	}
}
