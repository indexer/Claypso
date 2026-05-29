//go:build linux

package keychain

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
)

// TestLinuxAvailableUsesSecretTool pins the available() contract on Linux
// non-circularly: it stubs the lookPath seam (declared in keychain_linux.go)
// so the test never inspects the real PATH or runs `secret-tool`. It asserts
// that available() probes for the EXACT binary name "secret-tool" and maps the
// lookup result to the right boolean in both directions.
func TestLinuxAvailableUsesSecretTool(t *testing.T) {
	origLookPath := lookPath
	t.Cleanup(func() { lookPath = origLookPath })

	t.Run("present on PATH -> available", func(t *testing.T) {
		var gotName string
		lookPath = func(file string) (string, error) {
			gotName = file
			return "/usr/bin/secret-tool", nil
		}
		if !available() {
			t.Error("available() = false when secret-tool resolves, want true")
		}
		if gotName != "secret-tool" {
			t.Errorf("available() looked up %q, want %q", gotName, "secret-tool")
		}
	})

	t.Run("absent from PATH -> unavailable", func(t *testing.T) {
		var gotName string
		lookPath = func(file string) (string, error) {
			gotName = file
			return "", exec.ErrNotFound
		}
		if available() {
			t.Error("available() = true when secret-tool is missing, want false")
		}
		if gotName != "secret-tool" {
			t.Errorf("available() looked up %q, want %q", gotName, "secret-tool")
		}
	})
}

// TestLinuxAvailableExportedMatchesSeam checks the exported Available() wrapper
// reflects the stubbed seam in both directions, again without touching PATH.
func TestLinuxAvailableExportedMatchesSeam(t *testing.T) {
	origLookPath := lookPath
	t.Cleanup(func() { lookPath = origLookPath })

	lookPath = func(string) (string, error) { return "/usr/bin/secret-tool", nil }
	if !Available() {
		t.Error("Available() = false when secret-tool resolves, want true")
	}

	lookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	if Available() {
		t.Error("Available() = true when secret-tool is missing, want false")
	}
}

// fakeSecretTool replaces the execCommand seam with one that re-executes this
// test binary as a stand-in `secret-tool`, controlled by env vars. It returns
// a getter for the (name + args) the code under test invoked, so we can assert
// the command was constructed correctly. This exercises store/retrieve/forget
// hermetically — no live libsecret/D-Bus and no real credential written.
func fakeSecretTool(t *testing.T, mode string) func() []string {
	t.Helper()
	orig := execCommand
	var got []string
	execCommand = func(name string, args ...string) *exec.Cmd {
		got = append([]string{name}, args...)
		helperArgs := append([]string{"-test.run=TestHelperProcess", "--", name}, args...)
		c := exec.Command(os.Args[0], helperArgs...) //nolint:gosec // os.Args[0] is the test binary
		c.Env = append(os.Environ(), "GO_WANT_HELPER_PROCESS=1", "MOCK_MODE="+mode)
		return c
	}
	t.Cleanup(func() { execCommand = orig })
	return func() []string { return got }
}

// TestHelperProcess is not a real test: when GO_WANT_HELPER_PROCESS=1 it acts
// as a fake secret-tool. os.Exit short-circuits the test framework so the
// parent only sees what we write here (e.g. retrieve()'s captured stdout).
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") != "1" {
		return
	}
	switch os.Getenv("MOCK_MODE") {
	case "lookup":
		fmt.Fprintln(os.Stdout, "s3cr3t") // secret-tool prints the value + a trailing newline
	case "store":
		_, _ = io.Copy(io.Discard, os.Stdin) // secret-tool store reads the secret from stdin
	case "fail":
		os.Exit(1)
	}
	os.Exit(0)
}

func TestLinuxStore(t *testing.T) {
	getArgs := fakeSecretTool(t, "store")
	if err := store([]byte("hunter2")); err != nil {
		t.Fatalf("store: %v", err)
	}
	args := getArgs()
	if len(args) < 3 || args[0] != "secret-tool" || args[1] != "store" || args[len(args)-1] != ServiceName {
		t.Errorf("store ran unexpected command: %v", args)
	}
}

func TestLinuxRetrieveTrimsTrailingNewline(t *testing.T) {
	getArgs := fakeSecretTool(t, "lookup")
	got, err := retrieve()
	if err != nil {
		t.Fatalf("retrieve: %v", err)
	}
	if string(got) != "s3cr3t" {
		t.Errorf("retrieve = %q, want %q (trailing newline trimmed)", got, "s3cr3t")
	}
	if args := getArgs(); args[1] != "lookup" || args[len(args)-1] != ServiceName {
		t.Errorf("retrieve ran unexpected command: %v", args)
	}
}

func TestLinuxForget(t *testing.T) {
	getArgs := fakeSecretTool(t, "ok") // any non-fail mode exits 0
	if err := forget(); err != nil {
		t.Fatalf("forget: %v", err)
	}
	if args := getArgs(); args[1] != "clear" || args[len(args)-1] != ServiceName {
		t.Errorf("forget ran unexpected command: %v", args)
	}
}

func TestLinuxStoreRetrieveForgetSurfaceErrors(t *testing.T) {
	fakeSecretTool(t, "fail")
	if err := store([]byte("x")); err == nil {
		t.Error("store should surface a secret-tool failure")
	}
	fakeSecretTool(t, "fail")
	if _, err := retrieve(); err == nil {
		t.Error("retrieve should surface a secret-tool failure")
	}
	fakeSecretTool(t, "fail")
	if err := forget(); err == nil {
		t.Error("forget should surface a secret-tool failure")
	}
}
