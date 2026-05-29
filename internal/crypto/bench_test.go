package crypto

import "testing"

// BenchmarkDeriveKey tracks the deliberately-expensive Argon2id cost (the key
// derivation that dominates open/save latency).
func BenchmarkDeriveKey(b *testing.B) {
	pass := []byte("benchmark passphrase")
	salt := []byte("0123456789abcdef")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = DeriveKey(pass, salt)
	}
}

// BenchmarkEncryptDecrypt measures a full one-shot seal+open roundtrip
// (includes two Argon2id derivations, so it is intentionally slow).
func BenchmarkEncryptDecrypt(b *testing.B) {
	pass := []byte("benchmark passphrase")
	plain := []byte(`{"version":2,"projects":{}}`)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		blob, err := Encrypt(pass, plain)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := Decrypt(pass, blob); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkCipherSeal isolates the per-save cost once the key is derived (the
// common case: a Cipher is reused across saves in one process).
func BenchmarkCipherSeal(b *testing.B) {
	c, err := NewCipher([]byte("benchmark passphrase"))
	if err != nil {
		b.Fatal(err)
	}
	defer c.Clear()
	plain := []byte(`{"version":2,"projects":{}}`)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Seal(plain); err != nil {
			b.Fatal(err)
		}
	}
}
