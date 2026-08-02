package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yemon/calypso/internal/vault"
)

func TestBrokerCapabilityHelpers(t *testing.T) {
	if !validBearer("Bearer capability-token", "capability-token") {
		t.Fatal("valid bearer capability rejected")
	}
	for _, invalid := range []string{"", "capability-token", "Bearer wrong", "bearer capability-token"} {
		if validBearer(invalid, "capability-token") {
			t.Fatal("invalid bearer capability accepted")
		}
	}
	if _, err := parseConnectionMode("0600"); err != nil {
		t.Fatal(err)
	}
	if _, err := parseConnectionMode("0640"); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []string{"0644", "0666", "0777", "bad"} {
		if _, err := parseConnectionMode(invalid); err == nil {
			t.Fatalf("unsafe connection mode %q accepted", invalid)
		}
	}
}

func TestBrokerHandlerEnforcesTokenAllowlistAndNoArguments(t *testing.T) {
	h := &brokerHandler{
		vault:   vault.New(),
		token:   "capability-token",
		allowed: map[string]bool{"op_allowed": true},
	}
	call := func(token, path, body string) int {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if got := call("wrong", "/v1/operations/op_allowed", ""); got != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d", got)
	}
	if got := call("capability-token", "/v1/operations/op_other", ""); got != http.StatusNotFound {
		t.Fatalf("unallowed operation status = %d", got)
	}
	if got := call("capability-token", "/v1/operations/op_allowed", `{"arg":"dynamic"}`); got != http.StatusBadRequest {
		t.Fatalf("dynamic arguments status = %d", got)
	}
}

func TestBrokerConnectionFileIsBoundedAndNoClobber(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broker.json")
	conn := brokerConnection{
		Endpoint: "http://127.0.0.1:12345",
		Token:    "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG",
	}
	if err := writeBrokerConnection(path, conn, 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("connection file permissions are not owner-only")
	}
	got, err := readBrokerConnection(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != conn {
		t.Fatal("connection capability did not round-trip")
	}
	if err := writeBrokerConnection(path, conn, 0o600); err == nil {
		t.Fatal("connection writer overwrote an existing capability")
	}
}
