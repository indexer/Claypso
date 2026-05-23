// Package crypto provides authenticated encryption using Argon2id key
// derivation and NaCl secretbox. Every blob is self-describing:
// salt(16) || nonce(24) || ciphertext.
package crypto

import (
	"crypto/rand"
	"errors"
	"fmt"
	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/nacl/secretbox"
	"io"
)

const (
	saltLen  = 16
	nonceLen = 24
	keyLen   = 32

	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 4
)

// ErrDecrypt means either a wrong passphrase or a corrupted vault.
// The two are deliberately indistinguishable.
var ErrDecrypt = errors.New("decryption failed: wrong passphrase or corrupted vault")

// DeriveKey turns a passphrase + salt into a 32-byte symmetric key.
func DeriveKey(passphrase, salt []byte) [keyLen]byte {
	raw := argon2.IDKey(passphrase, salt, argonTime, argonMemory, argonThreads, keyLen)
	var key [keyLen]byte
	copy(key[:], raw)
	return key
}

// Encrypt produces a self-describing blob: salt || nonce || ciphertext.
// A fresh random salt and nonce are generated for every call.
func Encrypt(passphrase, plaintext []byte) ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("generating salt: %w", err)
	}

	var nonce [nonceLen]byte
	if _, err := io.ReadFull(rand.Reader, nonce[:]); err != nil {
		return nil, fmt.Errorf("generating nonce: %w", err)
	}

	key := DeriveKey(passphrase, salt)

	out := make([]byte, 0, saltLen+nonceLen+len(plaintext)+secretbox.Overhead)
	out = append(out, salt...)
	out = append(out, nonce[:]...)
	out = secretbox.Seal(out, plaintext, &nonce, &key)
	return out, nil
}

// Decrypt reverses Encrypt. Returns ErrDecrypt on any failure.
func Decrypt(passphrase, blob []byte) ([]byte, error) {
	if len(blob) < saltLen+nonceLen+secretbox.Overhead {
		return nil, ErrDecrypt
	}

	salt := blob[:saltLen]
	var nonce [nonceLen]byte
	copy(nonce[:], blob[saltLen:saltLen+nonceLen])
	ciphertext := blob[saltLen+nonceLen:]

	key := DeriveKey(passphrase, salt)

	plaintext, ok := secretbox.Open(nil, ciphertext, &nonce, &key)
	if !ok {
		return nil, ErrDecrypt
	}
	return plaintext, nil
}
