package project

import (
	"fmt"
	"strings"
	"testing"
)

// BenchmarkParseEnv measures parsing a realistic .env (hand-rolled scanner).
func BenchmarkParseEnv(b *testing.B) {
	content := strings.Repeat("KEY_X=some-value-here\n", 50) +
		"QUOTED=\"a b c\"\nMULTI=\"line1\\nline2\"\n"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ParseEnv(content)
	}
}

// BenchmarkSerializeEnv measures serialising 50 vars to .env, including the
// per-value memguard enclave Open done by Secret.Reveal.
func BenchmarkSerializeEnv(b *testing.B) {
	vars := make([]Var, 50)
	for i := range vars {
		vars[i] = Var{Key: fmt.Sprintf("KEY_%d", i), Value: SecretFromString("some-value-here")}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = SerializeEnv(vars)
	}
}

// BenchmarkSecretRoundtrip isolates the cost of sealing a value into an enclave
// and revealing it once.
func BenchmarkSecretRoundtrip(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s := SecretFromString("some-secret-value")
		_ = s.Reveal()
	}
}
