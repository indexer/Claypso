// Package crypto provides authenticated encryption using Argon2id key
// derivation and NaCl secretbox. Every blob is self-describing:
// salt(16) || nonce(24) || ciphertext.
package crypto

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"github.com/awnumar/memguard"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/nacl/secretbox"
)

const (
	saltLen  = 16
	nonceLen = 24
	keyLen   = 32

	argonTime    = 4
	argonMemory  = 128 * 1024
	argonThreads = 4
)

// ErrDecrypt means either a wrong passphrase or a corrupted vault.
// The two are deliberately indistinguishable.
var ErrDecrypt = errors.New("decryption failed: wrong passphrase or corrupted vault")

// Wipe zeroes b. Use it on key material or decrypted plaintext once it is no
// longer needed so secrets don't linger in memory longer than necessary.
func Wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// DeriveKey turns a passphrase + salt into a 32-byte symmetric key. The result
// is a freshly allocated, addressable slice so callers can Wipe it; we don't
// copy it into a [32]byte (whose value-copies would be unzeroable).
func DeriveKey(passphrase, salt []byte) []byte {
	return argon2.IDKey(passphrase, salt, argonTime, argonMemory, argonThreads, keyLen)
}

// Encrypt produces a self-describing blob: salt || nonce || ciphertext.
// A fresh random salt and nonce are generated for every call. This is the
// one-shot form; callers that re-encrypt repeatedly should derive a Cipher
// once (NewCipher / Open) and call Seal to avoid re-running Argon2id.
func Encrypt(passphrase, plaintext []byte) ([]byte, error) {
	c, err := NewCipher(passphrase)
	if err != nil {
		return nil, err
	}
	defer c.Clear() // one-shot: release the locked key buffer (memguard sets no finalizer)
	return c.Seal(plaintext)
}

// Decrypt reverses Encrypt. Returns ErrDecrypt on any failure.
func Decrypt(passphrase, blob []byte) ([]byte, error) {
	plain, c, err := Open(passphrase, blob)
	c.Clear() // one-shot: we keep no cipher, so release its locked key (Clear nil-guards)
	return plain, err
}

// Cipher binds a passphrase-derived key to its salt so a long-lived process
// can run the expensive Argon2id derivation once and then encrypt many times.
//
// Reusing the salt (and therefore the key) across encryptions is safe:
// secretbox only requires that the nonce be unique per key, and Seal draws a
// fresh random nonce on every call. This is what lets a single CLI invocation
// decrypt the vault and later re-encrypt it without a second key derivation.
type Cipher struct {
	key  *memguard.LockedBuffer // 32-byte derived key in mlock'd, guard-paged, off-heap memory
	salt []byte                 // saltLen bytes, owned by this Cipher
}

// newCipher moves a freshly derived key into a memguard LockedBuffer (mlock'd
// so it never reaches swap, guard-paged, and wiped on Destroy) and wipes the
// source slice. Argon2id's large scratch buffer is left unlocked — it isn't
// the secret and locking 128 MiB would be costly and fragile.
func newCipher(key, salt []byte) *Cipher {
	return &Cipher{key: memguard.NewBufferFromBytes(key), salt: salt}
}

// Clear destroys the locked key buffer (wipe + unlock + free). Call it when the
// Cipher is no longer needed — e.g. the read-only dashboard, which decrypts
// once and never re-encrypts. After Clear the Cipher must not Seal again.
func (c *Cipher) Clear() {
	if c != nil && c.key != nil {
		c.key.Destroy()
	}
}

// sealKey copies the key into a fixed-size array for secretbox, which requires
// a *[32]byte. The caller must Wipe the returned array when done.
func (c *Cipher) sealKey() [keyLen]byte {
	var k [keyLen]byte
	copy(k[:], c.key.Bytes())
	return k
}

// NewCipher derives a key from passphrase using a fresh random salt. Use it
// for a brand-new vault that has no on-disk salt yet.
func NewCipher(passphrase []byte) (*Cipher, error) {
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generating salt: %w", err)
	}
	return newCipher(DeriveKey(passphrase, salt), salt), nil
}

// Open decrypts a self-describing blob and returns the plaintext together with
// a Cipher carrying the blob's salt and derived key, so the caller can
// re-encrypt in the same process without deriving the key again.
func Open(passphrase, blob []byte) ([]byte, *Cipher, error) {
	if len(blob) < saltLen+nonceLen+secretbox.Overhead {
		return nil, nil, ErrDecrypt
	}

	salt := make([]byte, saltLen)
	copy(salt, blob[:saltLen])
	var nonce [nonceLen]byte
	copy(nonce[:], blob[saltLen:saltLen+nonceLen])
	ciphertext := blob[saltLen+nonceLen:]

	c := newCipher(DeriveKey(passphrase, salt), salt)

	k := c.sealKey()
	defer Wipe(k[:])
	plaintext, ok := secretbox.Open(nil, ciphertext, &nonce, &k)
	if !ok {
		c.key.Destroy() // don't leak a locked buffer on the wrong-passphrase path
		return nil, nil, ErrDecrypt
	}
	return plaintext, c, nil
}

// Seal encrypts plaintext into a self-describing blob (salt || nonce ||
// ciphertext), reusing the Cipher's salt and a freshly generated nonce. The
// salt matches Open/NewCipher so the output is decryptable by Decrypt/Open
// with the same passphrase.
func (c *Cipher) Seal(plaintext []byte) ([]byte, error) {
	var nonce [nonceLen]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}

	k := c.sealKey()
	defer Wipe(k[:])
	out := make([]byte, 0, saltLen+nonceLen+len(plaintext)+secretbox.Overhead)
	out = append(out, c.salt...)
	out = append(out, nonce[:]...)
	out = secretbox.Seal(out, plaintext, &nonce, &k)
	return out, nil
}
