package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testEvent(op string) Event {
	return Event{Time: "2026-08-02T10:00:00Z", Op: op, Spec: "myapp@dev", Keys: 3, Outcome: "ok", User: "tester"}
}

func TestAppendAndVerify(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	for _, op := range []string{"pull", "get-reveal", "sync-fly"} {
		if err := Append(path, testEvent(op)); err != nil {
			t.Fatalf("Append(%s): %v", op, err)
		}
	}
	n, err := Verify(path)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if n != 3 {
		t.Errorf("want 3 valid events, got %d", n)
	}
	events, err := Read(path)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if events[0].Prev != genesisPrev {
		t.Errorf("first event should link to genesis, got %q", events[0].Prev)
	}
	if events[2].Prev != events[1].Hash {
		t.Error("chain not linked")
	}
}

func TestVerifyDetectsEditedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	for i := 0; i < 3; i++ {
		if err := Append(path, testEvent("pull")); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(path)
	tampered := strings.Replace(string(data), `"keys":3`, `"keys":1`, 1)
	if tampered == string(data) {
		t.Fatal("tamper substitution failed")
	}
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(path); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Errorf("edited line should fail verification, got %v", err)
	}
}

func TestVerifyDetectsDeletedLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	for i := 0; i < 3; i++ {
		if err := Append(path, testEvent("pull")); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(path)
	lines := strings.SplitAfter(string(data), "\n")
	if err := os.WriteFile(path, []byte(lines[0]+lines[2]), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(path); err == nil || !strings.Contains(err.Error(), "chain broken") {
		t.Errorf("deleted line should fail verification, got %v", err)
	}
}

func TestEventNeverHoldsValues(t *testing.T) {
	// Guard against field creep: serialized events must stay metadata-only.
	line, err := json.Marshal(testEvent("pull"))
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"value", "passphrase", "secret"} {
		if strings.Contains(strings.ToLower(string(line)), banned) {
			t.Errorf("event JSON contains suspicious field %q: %s", banned, line)
		}
	}
}

func TestVerifyMissingFile(t *testing.T) {
	if _, err := Verify(filepath.Join(t.TempDir(), "none.log")); !os.IsNotExist(err) {
		t.Errorf("want IsNotExist, got %v", err)
	}
}
