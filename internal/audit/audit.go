// Package audit appends tamper-evident records of secret-touching
// operations to a JSON-lines log. Each line carries the SHA-256 of the
// previous line's hash plus its own payload, forming a chain: editing or
// deleting any line breaks verification from that point on. The log never
// contains secret values or passphrases — only operation metadata.
//
// The log is plaintext by design (it must be readable without unlocking the
// vault, e.g. for incident review), so it is evidence against accidents and
// honest-but-curious processes, not against an attacker with write access
// to the user's files.
package audit

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"

	"github.com/yemon/calypso/internal/lockfile"
)

// genesisPrev seeds the chain for the first event in a log.
const genesisPrev = "genesis"

// maxLineBytes bounds a single audit line; anything longer is corrupt.
const maxLineBytes = 1 << 20

// Event is one audit record. Hash covers the JSON encoding of the event
// with Hash itself empty; Prev is the previous event's Hash.
type Event struct {
	Time       string `json:"time"`
	Op         string `json:"op"`
	Spec       string `json:"spec,omitempty"`
	Keys       int    `json:"keys,omitempty"`
	Outcome    string `json:"outcome"` // "ok" or "denied"
	User       string `json:"user,omitempty"`
	ParentProc string `json:"parent,omitempty"`
	TTY        bool   `json:"tty"`
	Unattended bool   `json:"unattended"`
	Prev       string `json:"prev"`
	Hash       string `json:"hash"`
}

func computeHash(ev Event) (string, error) {
	ev.Hash = ""
	payload, err := json.Marshal(ev)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:]), nil
}

// Append writes ev to the log at path, linking it into the hash chain. The
// append is guarded by an advisory lock so concurrent calypso processes
// can't interleave the read-last-hash + write sequence.
func Append(path string, ev Event) error {
	lock, err := lockfile.Lock(path + ".lock")
	if err != nil {
		return fmt.Errorf("audit log locked: %w", err)
	}
	defer func() { _ = lockfile.Unlock(lock) }() //nolint:errcheck // release failure can't unwind an already-durable append

	prev, err := lastHash(path)
	if err != nil {
		return err
	}
	ev.Prev = prev
	ev.Hash, err = computeHash(ev)
	if err != nil {
		return err
	}
	line, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// lastHash returns the hash of the log's final event, genesisPrev for an
// empty or missing log. An unparseable final line yields an error rather
// than silently restarting the chain.
func lastHash(path string) (string, error) {
	events, err := Read(path)
	if err != nil {
		if os.IsNotExist(err) {
			return genesisPrev, nil
		}
		return "", err
	}
	if len(events) == 0 {
		return genesisPrev, nil
	}
	return events[len(events)-1].Hash, nil
}

// Read returns every event in the log in order. A missing file returns
// os.ErrNotExist; a malformed line returns an error naming its position.
func Read(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	n := 0
	for sc.Scan() {
		n++
		if len(sc.Bytes()) == 0 {
			continue
		}
		var ev Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			return nil, fmt.Errorf("audit log line %d is malformed: %w", n, err)
		}
		out = append(out, ev)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// Verify walks the chain and returns the number of valid events. It fails
// on the first event whose hash doesn't match its payload or whose Prev
// doesn't link to the preceding event.
func Verify(path string) (int, error) {
	events, err := Read(path)
	if err != nil {
		return 0, err
	}
	prev := genesisPrev
	for i, ev := range events {
		want, err := computeHash(ev)
		if err != nil {
			return i, err
		}
		if ev.Hash != want {
			return i, fmt.Errorf("event %d: hash mismatch (line edited?)", i+1)
		}
		if ev.Prev != prev {
			return i, fmt.Errorf("event %d: chain broken (line deleted or reordered before it?)", i+1)
		}
		prev = ev.Hash
	}
	return len(events), nil
}
