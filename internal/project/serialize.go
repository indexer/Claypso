package project

import (
	"os"
	"strings"
)

// serialize writes a header followed by KEY=value lines produced by valueFor.
func serialize(header string, vars []Var, valueFor func(Var) string) string {
	var b strings.Builder
	b.Grow(len(header) + len(vars)*32)
	b.WriteString(header)
	for _, v := range vars {
		b.WriteString(v.Key)
		b.WriteByte('=')
		b.WriteString(valueFor(v))
		b.WriteByte('\n')
	}
	return b.String()
}

func quotedIfNeeded(v Var) string {
	if needsQuoting(v.Value) {
		return "\"" + escape(v.Value) + "\""
	}
	return v.Value
}

// SerializeEnv produces .env file content from variables.
func SerializeEnv(vars []Var) string {
	const header = "# Managed by calypso. Generated file — edit via `calypso set` then `calypso pull`.\n"
	return serialize(header, vars, quotedIfNeeded)
}

// SerializeSafeEnv produces .env content with all values replaced by SafePlaceholder.
func SerializeSafeEnv(vars []Var) string {
	const header = "# Managed by calypso. Safe mode — values are masked. Run `calypso pull` to restore.\n"
	return serialize(header, vars, func(Var) string { return SafePlaceholder })
}

// SerializeExampleEnv renders variable keys with empty values, suitable for
// a .env.example template.
func SerializeExampleEnv(vars []Var) string {
	const header = "# Managed by calypso. Example — fill in values and `calypso push` to import.\n"
	return serialize(header, vars, func(Var) string { return "" })
}

// ReadEnvFile reads and parses a .env file.
func ReadEnvFile(path string) ([]Var, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseEnv(string(data)), nil
}

// atomicWriteFile writes data to path durably: it writes a sibling temp file
// and renames it into place, so a reader (or a crash mid-write) never sees a
// half-written or truncated file. This mirrors the vault's own write contract
// for the .env files calypso generates, which a plain os.WriteFile would
// otherwise truncate in place.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// WriteEnvFile writes variables to disk with owner-only permissions.
func WriteEnvFile(path string, vars []Var) error {
	return atomicWriteFile(path, []byte(SerializeEnv(vars)), 0o600)
}

// WriteSafeEnvFile writes with values replaced by SafePlaceholder.
func WriteSafeEnvFile(path string, vars []Var) error {
	return atomicWriteFile(path, []byte(SerializeSafeEnv(vars)), 0o600)
}

// WriteExampleEnvFile writes a .env.example template with empty values. The
// template holds no secrets, so it is intentionally world-readable (0o644).
func WriteExampleEnvFile(path string, vars []Var) error {
	return atomicWriteFile(path, []byte(SerializeExampleEnv(vars)), 0o644)
}
