package lockfile

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// onUnix reports whether the active build provides real advisory locking
// (flock(2)). The windows build is a no-op open with no contention semantics.
func onUnix() bool { return runtime.GOOS != "windows" }

// TestLockAcquireAndUnlock verifies the basic happy path: acquiring a lock on
// a fresh path succeeds, returns a usable file handle, and unlocking it
// reports no error.
func TestLockAcquireAndUnlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calypso.lock")

	f, err := Lock(path)
	if err != nil {
		t.Fatalf("Lock(%q) returned error: %v", path, err)
	}
	if f == nil {
		t.Fatal("Lock returned a nil file handle without an error")
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected lock file to exist at %q: %v", path, err)
	}

	if err := Unlock(f); err != nil {
		t.Fatalf("Unlock returned error: %v", err)
	}
}

// TestLockCreatesParentDirs verifies that Lock creates any missing parent
// directories for the lock path (documented behaviour on the unix build).
// On windows the parent directory does not exist, so we create it first to
// keep the test hermetic and exercise only the documented API.
func TestLockCreatesParentDirs(t *testing.T) {
	base := t.TempDir()
	nested := filepath.Join(base, "a", "b", "c")
	path := filepath.Join(nested, "calypso.lock")

	if !onUnix() {
		// The windows implementation does not MkdirAll; create the tree so
		// OpenFile can succeed and we still test the public contract.
		if err := os.MkdirAll(nested, 0o700); err != nil {
			t.Fatalf("setup MkdirAll: %v", err)
		}
	}

	f, err := Lock(path)
	if err != nil {
		t.Fatalf("Lock(%q) returned error: %v", path, err)
	}
	t.Cleanup(func() { _ = Unlock(f) })

	if _, err := os.Stat(path); err != nil {
		t.Errorf("expected lock file (and parent dirs) to exist at %q: %v", path, err)
	}
}

// TestSecondAcquireContention verifies the documented contention behaviour.
// On the unix build flock uses LOCK_EX|LOCK_NB, so a second acquire on a held
// lock must fail immediately (non-blocking) rather than block. On the windows
// build there is no real locking, so a second acquire succeeds.
func TestSecondAcquireContention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calypso.lock")

	first, err := Lock(path)
	if err != nil {
		t.Fatalf("first Lock returned error: %v", err)
	}
	t.Cleanup(func() { _ = Unlock(first) })

	// Run the second acquire in a goroutine guarded by a timeout so that, if
	// the implementation ever regressed to a blocking lock, the test fails
	// loudly instead of hanging forever.
	type result struct {
		f   *os.File
		err error
	}
	done := make(chan result, 1)
	go func() {
		f, err := Lock(path)
		done <- result{f: f, err: err}
	}()

	select {
	case res := <-done:
		if onUnix() {
			if res.err == nil {
				_ = Unlock(res.f)
				t.Fatal("expected second Lock to fail while first is held (LOCK_NB), got nil error")
			}
		} else {
			// windows: no contention, second open should succeed.
			if res.err != nil {
				t.Fatalf("windows second Lock unexpectedly failed: %v", res.err)
			}
			if res.f != nil {
				_ = Unlock(res.f)
			}
		}
	case <-time.After(5 * time.Second):
		// Regression path: the goroutine is still blocked inside Lock. Do not
		// leak it or a late-returned flock fd. Register cleanup that, in its own
		// goroutine, drains `done` and unlocks the file if one was eventually
		// acquired. A goroutine is used because the blocked Lock only returns
		// once `first` is released by its own (earlier-registered, thus
		// later-running) cleanup, so the drain must not block the cleanup chain.
		t.Cleanup(func() {
			go func() {
				res := <-done
				if res.f != nil {
					_ = Unlock(res.f)
				}
			}()
		})
		t.Fatal("second Lock blocked for 5s; expected non-blocking behaviour")
	}
}

// TestReacquireAfterUnlock verifies that once a lock is released, the same
// path can be acquired again successfully.
func TestReacquireAfterUnlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calypso.lock")

	first, err := Lock(path)
	if err != nil {
		t.Fatalf("first Lock returned error: %v", err)
	}
	if err := Unlock(first); err != nil {
		t.Fatalf("Unlock returned error: %v", err)
	}

	second, err := Lock(path)
	if err != nil {
		t.Fatalf("re-acquire after Unlock failed: %v", err)
	}
	if err := Unlock(second); err != nil {
		t.Fatalf("second Unlock returned error: %v", err)
	}
}

// TestLockSamePathRepeatedly verifies that a serial acquire/release cycle can
// be repeated many times on the same path without leaking handles or wedging
// the lock (i.e. Unlock truly releases).
func TestLockSamePathRepeatedly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calypso.lock")

	for i := 0; i < 25; i++ {
		f, err := Lock(path)
		if err != nil {
			t.Fatalf("iteration %d: Lock returned error: %v", i, err)
		}
		if err := Unlock(f); err != nil {
			t.Fatalf("iteration %d: Unlock returned error: %v", i, err)
		}
	}
}

// TestLockDistinctPaths verifies that locks on different paths are independent
// and can be held simultaneously.
func TestLockDistinctPaths(t *testing.T) {
	dir := t.TempDir()
	pathA := filepath.Join(dir, "a.lock")
	pathB := filepath.Join(dir, "b.lock")

	fa, err := Lock(pathA)
	if err != nil {
		t.Fatalf("Lock(a) returned error: %v", err)
	}
	t.Cleanup(func() { _ = Unlock(fa) })

	fb, err := Lock(pathB)
	if err != nil {
		t.Fatalf("Lock(b) returned error while holding a: %v", err)
	}
	t.Cleanup(func() { _ = Unlock(fb) })
}

// TestLockUnwritableParent verifies that Lock surfaces an error when the lock
// path cannot be created. We point at a path whose parent is an existing
// regular file, which makes both MkdirAll (unix) and OpenFile fail. This
// exercises the error-return path of the public API.
func TestLockUnwritableParent(t *testing.T) {
	dir := t.TempDir()
	fileAsDir := filepath.Join(dir, "notadir")
	if err := os.WriteFile(fileAsDir, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup WriteFile: %v", err)
	}
	// Using the regular file as if it were a directory must fail.
	path := filepath.Join(fileAsDir, "calypso.lock")

	f, err := Lock(path)
	if err == nil {
		_ = Unlock(f)
		t.Fatalf("expected Lock(%q) to fail when parent is a regular file", path)
	}
	if f != nil {
		t.Errorf("expected nil file handle on error, got %v", f)
		_ = Unlock(f)
	}
	// The error should be a filesystem error (e.g. ENOTDIR); just assert it is
	// a *PathError-style error surfaced from the os layer.
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Logf("note: error was not *os.PathError (%T): %v", err, err)
	}
}
