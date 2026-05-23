package crypto

import (
	"bytes"
	"crypto/rand"
	"testing"
)

func TestEncryptDecryptRoundtrip(t *testing.T) {
	pass := []byte("correct horse battery staple")
	plain := []byte(`{"projects":{"test":{"name":"test","path":".env","vars":[]}}}`)

	cipher, err := Encrypt(pass, plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	got, err := Decrypt(pass, cipher)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if !bytes.Equal(got, plain) {
		t.Errorf("roundtrip mismatch:\nwant %s\ngot  %s", plain, got)
	}
}

func TestDecryptWrongPassphrase(t *testing.T) {
	pass := []byte("correct horse battery staple")
	wrong := []byte("wrong passphrase")
	plain := []byte("hello world")

	cipher, err := Encrypt(pass, plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	_, err = Decrypt(wrong, cipher)
	if err != ErrDecrypt {
		t.Errorf("expected ErrDecrypt, got %v", err)
	}
}

func TestDecryptTamperedBlob(t *testing.T) {
	pass := []byte("my passphrase")
	plain := []byte("sensitive data here")

	cipher, err := Encrypt(pass, plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if len(cipher) < 1 {
		t.Fatal("ciphertext too short")
	}
	cipher[len(cipher)-1] ^= 0xFF

	_, err = Decrypt(pass, cipher)
	if err != ErrDecrypt {
		t.Errorf("expected ErrDecrypt for tampered blob, got %v", err)
	}
}

func TestDecryptTooShort(t *testing.T) {
	pass := []byte("my passphrase")
	short := []byte("x")
	_, err := Decrypt(pass, short)
	if err != ErrDecrypt {
		t.Errorf("expected ErrDecrypt for short blob, got %v", err)
	}
}

func TestEncryptProducesUniqueSalt(t *testing.T) {
	pass := []byte("same passphrase")
	plain := []byte("same plaintext")

	c1, err := Encrypt(pass, plain)
	if err != nil {
		t.Fatalf("first encrypt: %v", err)
	}
	c2, err := Encrypt(pass, plain)
	if err != nil {
		t.Fatalf("second encrypt: %v", err)
	}

	if bytes.Equal(c1, c2) {
		t.Error("two encryptions of same input should produce different ciphertexts")
	}
}

func TestEncryptDecryptEmpty(t *testing.T) {
	pass := []byte("secret")
	plain := []byte{}

	cipher, err := Encrypt(pass, plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	got, err := Decrypt(pass, cipher)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if !bytes.Equal(got, plain) {
		t.Error("empty roundtrip failed")
	}
}

func TestEncryptDecryptLargePayload(t *testing.T) {
	pass := []byte("secret key")
	plain := make([]byte, 1024*1024)
	if _, err := rand.Read(plain); err != nil {
		t.Fatalf("rand read: %v", err)
	}

	cipher, err := Encrypt(pass, plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	got, err := Decrypt(pass, cipher)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if !bytes.Equal(got, plain) {
		t.Error("large payload roundtrip failed")
	}
}

func TestDeriveKeyDeterministic(t *testing.T) {
	pass := []byte("deterministic test")
	salt := []byte("1234567890123456")

	k1 := DeriveKey(pass, salt)
	k2 := DeriveKey(pass, salt)

	if k1 != k2 {
		t.Error("DeriveKey should be deterministic for same inputs")
	}
}

func TestDeriveKeyDifferentSalt(t *testing.T) {
	pass := []byte("deterministic test")
	salt1 := []byte("aaaaaaaaaaaaaaaa")
	salt2 := []byte("bbbbbbbbbbbbbbbb")

	k1 := DeriveKey(pass, salt1)
	k2 := DeriveKey(pass, salt2)

	if k1 == k2 {
		t.Error("different salts should produce different keys")
	}
}
