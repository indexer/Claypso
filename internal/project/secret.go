package project

import (
	"encoding/json"

	"github.com/awnumar/memguard"
)

// Secret holds a variable's value in encrypted-at-rest memory (a memguard
// Enclave): the plaintext is sealed with a process-global key and materialised
// only transiently, via Reveal, when the value is actually used. Stored this
// way, secret values never linger as plain Go strings in the long-lived vault
// struct. The plaintext byte length is kept alongside (length isn't sensitive)
// so validation needn't open the enclave.
//
// Ceiling: Reveal copies the plaintext into an (unwipeable, GC-managed) Go
// string, and calypso writes values to plaintext .env files by design — so this
// reduces in-RAM residency; it does not make secrets unrecoverable from an
// attacker who already has the user's filesystem.
type Secret struct {
	enc *memguard.Enclave
	n   int // plaintext byte length
}

// NewSecret seals v into an enclave and wipes v. An empty input yields the zero
// Secret (no enclave), which Reveals to "".
func NewSecret(v []byte) Secret {
	if len(v) == 0 {
		return Secret{}
	}
	n := len(v)
	return Secret{enc: memguard.NewEnclave(v), n: n}
}

// SecretFromString seals a Go string. The source string cannot be wiped (Go
// strings are immutable) — the unavoidable ceiling at input boundaries such as
// CLI args and parsed .env files.
func SecretFromString(s string) Secret {
	return NewSecret([]byte(s))
}

// Reveal returns the plaintext as a string. Use it only at the moment of use
// (serialise, display, compare) and don't retain the result.
func (s Secret) Reveal() string {
	if s.enc == nil {
		return ""
	}
	buf, err := s.enc.Open()
	if err != nil {
		return ""
	}
	defer buf.Destroy()
	return string(buf.Bytes())
}

// Len reports the plaintext byte length without opening the enclave.
func (s Secret) Len() int { return s.n }

// IsZero reports an unset/empty value.
func (s Secret) IsZero() bool { return s.enc == nil }

// MarshalJSON renders the plaintext as a JSON string. The output is destined
// for the encrypted vault blob, which is itself sealed and wiped after writing.
func (s Secret) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.Reveal())
}

// UnmarshalJSON reads a JSON string value into a freshly sealed enclave.
func (s *Secret) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}
	*s = SecretFromString(str)
	return nil
}
