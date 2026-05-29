package vault

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// isLockContention reports whether err is the non-blocking advisory lock
// reporting that another holder has the vault. withLock wraps it with this
// message; a contended Lock/Save returns before running its body, so callers
// can cheaply retry rather than corrupting the file.
func isLockContention(err error) bool {
	return err != nil && strings.Contains(err.Error(), "locked by another process")
}

// retryable reports whether a failed mutate-and-save can simply be retried with
// a fresh Load: either we lost the non-blocking lock race, or compare-and-swap
// detected a concurrent write. Both mean "reload the latest state and re-apply".
func retryable(err error) bool {
	return isLockContention(err) || errors.Is(err, ErrConflict)
}

// TestConcurrentVaultAccessNoLostUpdates exercises the full lock +
// compare-and-swap write path under contention. Many writers each run a
// load-modify-save adding their own project, retrying on lock contention or a
// CAS conflict; readers hammer raw reads meanwhile. Guarantees:
//
//   - No data race (run with -race) across the load/derive/save path.
//   - Atomic (temp+rename) writes mean a reader never sees an empty/truncated
//     vault mid-write.
//   - NO LOST UPDATES: because Save is compare-and-swap and workers reload on
//     conflict, every worker's project is present at the end — the
//     load-modify-save race is closed.
func TestConcurrentVaultAccessNoLostUpdates(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("super secret")
	if _, err := Init(bg, path, pw); err != nil {
		t.Fatalf("Init: %v", err)
	}

	const (
		writers        = 4
		readers        = 3
		readsPerReader = 200
	)

	var wg sync.WaitGroup
	errs := make(chan error, writers+readers)

	for w := 0; w < writers; w++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := fmt.Sprintf("proj%d", n)
			for attempt := 0; attempt < 3000; attempt++ {
				v, err := Load(bg, path, pw)
				if err != nil {
					if isLockContention(err) {
						time.Sleep(time.Millisecond)
						continue
					}
					errs <- fmt.Errorf("%s load: %w", name, err)
					return
				}
				if _, _, err := v.AddProject(name, "", "/tmp/"+name+"/.env"); err != nil {
					errs <- fmt.Errorf("%s add: %w", name, err)
					return
				}
				switch err := v.Save(bg, path, pw); {
				case err == nil:
					return
				case retryable(err):
					time.Sleep(time.Millisecond) // reload latest state and re-apply
					continue
				default:
					errs <- fmt.Errorf("%s save: %w", name, err)
					return
				}
			}
			errs <- fmt.Errorf("%s: gave up after retry budget", name)
		}(w)
	}

	for r := 0; r < readers; r++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			for i := 0; i < readsPerReader; i++ {
				blob, err := os.ReadFile(path)
				if err != nil {
					errs <- fmt.Errorf("reader %d ReadFile: %w", n, err)
					return
				}
				if len(blob) == 0 {
					errs <- fmt.Errorf("reader %d saw an empty vault mid-write (non-atomic write)", n)
					return
				}
				time.Sleep(time.Millisecond)
			}
		}(r)
	}

	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	final, err := Load(bg, path, pw)
	if err != nil {
		t.Fatalf("final Load failed — vault corrupted under concurrency: %v", err)
	}
	if got := len(final.Names()); got != writers {
		t.Errorf("expected %d projects with no lost updates, got %d: %v", writers, got, final.Names())
	}
	for i := 0; i < writers; i++ {
		if _, err := final.Project(fmt.Sprintf("proj%d", i)); err != nil {
			t.Errorf("proj%d missing — a concurrent update was lost: %v", i, err)
		}
	}
}

// TestSaveDetectsConcurrentModification is the deterministic core of the
// compare-and-swap guard: two vaults loaded from the same state, the first
// saves, and the second's save must be refused with ErrConflict rather than
// clobbering the first writer's change.
func TestSaveDetectsConcurrentModification(t *testing.T) {
	path := vaultFile(t)
	pw := []byte("super secret")
	if _, err := Init(bg, path, pw); err != nil {
		t.Fatalf("Init: %v", err)
	}

	v1, err := Load(bg, path, pw)
	if err != nil {
		t.Fatalf("Load v1: %v", err)
	}
	v2, err := Load(bg, path, pw)
	if err != nil {
		t.Fatalf("Load v2: %v", err)
	}

	if _, _, err := v1.AddProject("first", "", "/tmp/first/.env"); err != nil {
		t.Fatalf("v1 AddProject: %v", err)
	}
	if err := v1.Save(bg, path, pw); err != nil {
		t.Fatalf("v1 Save: %v", err)
	}

	if _, _, err := v2.AddProject("second", "", "/tmp/second/.env"); err != nil {
		t.Fatalf("v2 AddProject: %v", err)
	}
	if err := v2.Save(bg, path, pw); !errors.Is(err, ErrConflict) {
		t.Fatalf("v2 Save should have returned ErrConflict, got %v", err)
	}

	// v1's change survived; v2 did not clobber it.
	final, err := Load(bg, path, pw)
	if err != nil {
		t.Fatalf("final Load: %v", err)
	}
	if _, err := final.Project("first"); err != nil {
		t.Errorf("first writer's project was lost: %v", err)
	}
	if _, err := final.Project("second"); err == nil {
		t.Error("second writer clobbered the vault despite the conflict")
	}
}
