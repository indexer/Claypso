package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type strictFixture struct {
	bin       string
	vaultPath string
	envPath   string
	pass      string
	key       string
	value     string
	opID      string
}

func randomRuntimeHex(t *testing.T, prefix string, bytesN int) string {
	t.Helper()
	raw := make([]byte, bytesN)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal("runtime randomness unavailable")
	}
	return prefix + hex.EncodeToString(raw)
}

func setupStrictFixture(t *testing.T, endpoint, key, value string) strictFixture {
	t.Helper()
	f := strictFixture{
		bin:       calypsoPath(t),
		vaultPath: filepath.Join(t.TempDir(), "vault.enc"),
		pass:      randomRuntimeHex(t, "p_", 24),
		key:       key,
		value:     value,
	}
	f.envPath = filepath.Join(filepath.Dir(f.vaultPath), "website.env")
	if _, _, err := runCalypso(t, f.bin, f.pass,
		"--vault", f.vaultPath, "add", "website", "--path", f.envPath); err != nil {
		t.Fatalf("strict fixture add failed: %v", err)
	}
	if _, _, err := runCalypso(t, f.bin, f.pass,
		"--vault", f.vaultPath, "set", "website", f.key+"="+f.value); err != nil {
		t.Fatalf("strict fixture set failed: %v", err)
	}
	stdout, _, err := runCalypso(t, f.bin, f.pass,
		"--vault", f.vaultPath, "operation", "add-http", "website-check", "website",
		"--url", endpoint, "--key", f.key, "--expect", "200")
	if err != nil {
		t.Fatalf("strict fixture operation add failed: %v", err)
	}
	id := regexp.MustCompile(`op_[0-9a-f]{32}`).FindString(stdout)
	if id == "" {
		t.Fatal("operation add did not return an opaque ID")
	}
	f.opID = id
	if _, _, err := runCalypso(t, f.bin, f.pass,
		"--vault", f.vaultPath, "pull", "website", "--safe"); err != nil {
		t.Fatalf("strict fixture safe pull failed: %v", err)
	}
	if _, _, err := runCalypso(t, f.bin, f.pass,
		"--vault", f.vaultPath, "strict", "on"); err != nil {
		t.Fatalf("strict enable failed: %v", err)
	}
	return f
}

func assertProtectedAbsent(t *testing.T, f strictFixture, outputs ...string) {
	t.Helper()
	for _, output := range outputs {
		if strings.Contains(output, f.key) || strings.Contains(output, f.value) || strings.Contains(output, f.pass) {
			t.Fatal("agent-visible output contained protected credential material")
		}
	}
}

func TestIntegration_AgentStrictBlocksOwnerAndArbitraryExecution(t *testing.T) {
	var authenticated atomic.Int32
	key := strings.ToUpper(randomRuntimeHex(t, "S_", 12))
	value := randomRuntimeHex(t, "v_", 32)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer "+value {
			authenticated.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(key + "=" + value))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstream.Close()
	f := setupStrictFixture(t, upstream.URL, key, value)

	encryptedCopy := filepath.Join(t.TempDir(), "copy.enc")
	blob, err := os.ReadFile(f.vaultPath)
	if err != nil {
		t.Fatal("read encrypted fixture vault")
	}
	if err := os.WriteFile(encryptedCopy, blob, 0o600); err != nil {
		t.Fatal("write encrypted fixture copy")
	}

	blocked := [][]string{
		{"get", "website"},
		{"set", "website", "OTHER=value"},
		{"pull", "website", "--safe"},
		{"pull", "website", "--inject", "--", "sh", "-c", "exit 0"},
		{"diff", "website", "website"},
		{"gaps"},
		{"drift", "website"},
		{"exposure", "website"},
		{"dashboard"},
		{"sync", "fly", "website", "--dry-run"},
		{"operation", "remove", f.opID},
		{"export", "--base64"},
		{"import", encryptedCopy},
		{"vault", "backups", "restore", "missing.enc", "--force"},
		{"keychain", "forget"},
	}
	for _, args := range blocked {
		full := append([]string{"--vault", f.vaultPath}, args...)
		stdout, stderr, err := runCalypso(t, f.bin, f.pass, full...)
		if err == nil {
			t.Fatalf("strict mode allowed owner command %q", args[0])
		}
		if !strings.Contains(stderr, "strict") {
			t.Fatalf("owner command %q did not report strict denial", args[0])
		}
		assertProtectedAbsent(t, f, stdout, stderr)
	}
	strictEnv, err := os.ReadFile(f.envPath)
	if err != nil {
		t.Fatal("strict mode did not retain a safe sentinel")
	}
	assertProtectedAbsent(t, f, string(strictEnv))
	if strings.Contains(string(strictEnv), "=") {
		t.Fatal("strict-mode environment sentinel exposed variable names")
	}

	for _, args := range [][]string{
		{"list"},
		{"env", "list", "website"},
		{"strict", "status"},
		{"lockdown", "status"},
		{"operation", "list"},
		{"audit", "list"},
		{"audit", "verify"},
	} {
		full := append([]string{"--vault", f.vaultPath}, args...)
		stdout, stderr, err := runCalypso(t, f.bin, f.pass, full...)
		if err != nil {
			t.Fatalf("strict-safe command %q failed: %v", args[0], err)
		}
		assertProtectedAbsent(t, f, stdout, stderr)
	}
	opList, _, err := runCalypso(t, f.bin, f.pass,
		"--vault", f.vaultPath, "operation", "list")
	if err != nil {
		t.Fatal("strict operation list failed")
	}
	if strings.Contains(opList, "website-check") {
		t.Fatal("strict operation list exposed the owner-facing label")
	}

	stdout, stderr, err := runCalypso(t, f.bin, f.pass,
		"--vault", f.vaultPath, "operation", "run", f.opID)
	if err != nil {
		t.Fatalf("trusted operation failed: %v", err)
	}
	assertProtectedAbsent(t, f, stdout, stderr)
	if authenticated.Load() != 1 {
		t.Fatal("trusted operation did not authenticate with the real vault value")
	}

	if stdout, stderr, err := passphraseOnly(t, f.bin,
		"--vault", f.vaultPath, "strict", "off"); err == nil {
		t.Fatal("headless agent disabled strict mode")
	} else {
		assertProtectedAbsent(t, f, stdout, stderr)
	}
}

func TestIntegration_BrokerInvokesWithoutVaultCredentialAccess(t *testing.T) {
	var authenticated atomic.Int32
	key := strings.ToUpper(randomRuntimeHex(t, "S_", 12))
	value := randomRuntimeHex(t, "v_", 32)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer "+value {
			authenticated.Add(1)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(key + "=" + value))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer upstream.Close()
	f := setupStrictFixture(t, upstream.URL, key, value)

	connectionFile := filepath.Join(t.TempDir(), "broker.json")
	var brokerOut, brokerErr bytes.Buffer
	broker := exec.Command(f.bin, "--vault", f.vaultPath,
		"broker", "serve", "--connection-file", connectionFile, "--allow", f.opID)
	broker.Env = append(os.Environ(), "CALYPSO_PASSPHRASE="+f.pass)
	broker.Stdout = &brokerOut
	broker.Stderr = &brokerErr
	if err := broker.Start(); err != nil {
		t.Fatal("start broker")
	}
	t.Cleanup(func() {
		if broker.Process != nil {
			_ = broker.Process.Kill()
		}
	})

	deadline := time.Now().Add(10 * time.Second)
	for {
		if info, err := os.Stat(connectionFile); err == nil && info.Size() > 0 {
			if info.Mode().Perm() != 0o600 {
				t.Fatal("broker connection file is not owner-only")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("broker capability was not published")
		}
		time.Sleep(20 * time.Millisecond)
	}

	stdout, stderr, err := runCalypsoEnv(t, f.bin, nil,
		"broker", "invoke", f.opID, "--connection-file", connectionFile)
	if err != nil {
		t.Fatalf("credential-free broker invocation failed: %v", err)
	}
	assertProtectedAbsent(t, f, stdout, stderr)
	if authenticated.Load() != 1 {
		t.Fatal("broker did not authenticate using the real vault value")
	}
	if info, err := os.Stat(f.vaultPath); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatal("encrypted vault is not owner-only")
	}
	stdout, stderr, err = runCalypsoEnv(t, f.bin,
		[]string{"CALYPSO_PASSPHRASE=definitely-not-the-vault-passphrase"},
		"--vault", f.vaultPath, "list")
	if err == nil {
		t.Fatal("agent without the vault credential opened the vault directly")
	}
	assertProtectedAbsent(t, f, stdout, stderr)

	if err := broker.Process.Signal(os.Interrupt); err != nil {
		t.Fatal("interrupt broker")
	}
	waited := make(chan error, 1)
	go func() { waited <- broker.Wait() }()
	select {
	case err := <-waited:
		if err != nil {
			t.Fatal("broker did not shut down cleanly")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("broker did not stop after interrupt")
	}
	if _, err := os.Stat(connectionFile); !os.IsNotExist(err) {
		t.Fatal("broker capability file remained after shutdown")
	}
	assertProtectedAbsent(t, f, brokerOut.String(), brokerErr.String())

	// A non-catchable termination may leave the capability file, but strict
	// broker execution must still leave no credential material in workspace
	// files because it never materializes an environment file.
	crashConnection := filepath.Join(t.TempDir(), "broker-crash.json")
	crashed := exec.Command(f.bin, "--vault", f.vaultPath,
		"broker", "serve", "--connection-file", crashConnection, "--allow", f.opID)
	crashed.Env = append(os.Environ(), "CALYPSO_PASSPHRASE="+f.pass)
	if err := crashed.Start(); err != nil {
		t.Fatal("start crash-safety broker")
	}
	deadline = time.Now().Add(10 * time.Second)
	for {
		if info, err := os.Stat(crashConnection); err == nil && info.Size() > 0 {
			break
		}
		if time.Now().After(deadline) {
			_ = crashed.Process.Kill()
			t.Fatal("crash-safety broker capability was not published")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := crashed.Process.Kill(); err != nil {
		t.Fatal("kill crash-safety broker")
	}
	_ = crashed.Wait()
	err = filepath.WalkDir(filepath.Dir(f.vaultPath), func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		assertProtectedAbsent(t, f, string(data))
		return nil
	})
	if err != nil {
		t.Fatal("scan crash-safety workspace")
	}
}
