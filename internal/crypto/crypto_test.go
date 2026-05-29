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

	if !bytes.Equal(k1, k2) {
		t.Error("DeriveKey should be deterministic for same inputs")
	}
}

func TestOpenReturnsCipherThatRoundtrips(t *testing.T) {
	pass := []byte("correct horse battery staple")
	plain := []byte("first payload")

	blob, err := Encrypt(pass, plain)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	got, c, err := Open(pass, blob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("open roundtrip mismatch: want %q got %q", plain, got)
	}

	// Re-sealing with the returned Cipher must remain decryptable with the
	// same passphrase (proving the salt was carried over correctly).
	resealed, err := c.Seal([]byte("second payload"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	back, err := Decrypt(pass, resealed)
	if err != nil {
		t.Fatalf("decrypt resealed: %v", err)
	}
	if string(back) != "second payload" {
		t.Errorf("resealed roundtrip mismatch: got %q", back)
	}
}

func TestCipherSealReusesSaltWithFreshNonce(t *testing.T) {
	pass := []byte("a passphrase")
	blob, err := Encrypt(pass, []byte("payload"))
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	_, c, err := Open(pass, blob)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	out1, _ := c.Seal([]byte("payload"))
	out2, _ := c.Seal([]byte("payload"))

	// Salt (first saltLen bytes) is reused — that's the whole point: the key
	// need not be re-derived.
	if !bytes.Equal(out1[:saltLen], blob[:saltLen]) {
		t.Error("Seal should reuse the salt carried by the Cipher")
	}
	if !bytes.Equal(out1[:saltLen], out2[:saltLen]) {
		t.Error("repeated Seal calls should share the salt")
	}
	// Nonce (next nonceLen bytes) must differ so identical plaintexts don't
	// produce identical ciphertexts under the same key.
	if bytes.Equal(out1[saltLen:saltLen+nonceLen], out2[saltLen:saltLen+nonceLen]) {
		t.Error("Seal must use a fresh nonce each call")
	}
	if bytes.Equal(out1, out2) {
		t.Error("two seals of the same plaintext should differ (fresh nonce)")
	}
}

func TestDeriveKeyDifferentSalt(t *testing.T) {
	pass := []byte("deterministic test")
	salt1 := []byte("aaaaaaaaaaaaaaaa")
	salt2 := []byte("bbbbbbbbbbbbbbbb")

	k1 := DeriveKey(pass, salt1)
	k2 := DeriveKey(pass, salt2)

	if bytes.Equal(k1, k2) {
		t.Error("different salts should produce different keys")
	}
}

func TestCipherClearZeroesKey(t *testing.T) {
	c, err := NewCipher([]byte("passphrase for clear test"))
	if err != nil {
		t.Fatalf("NewCipher: %v", err)
	}
	if _, err := c.Seal([]byte("payload")); err != nil {
		t.Fatalf("Seal before Clear: %v", err)
	}
	c.Clear()
	for i, b := range c.key {
		if b != 0 {
			t.Fatalf("Clear should zero the derived key; byte %d = %d", i, b)
		}
	}
}
