// Package project models a registered project and its .env variables.
package project

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
)

const SafePlaceholder = "****"

// Var is a single environment variable.
type Var struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Project is a registered project with its .env path and variables.
type Project struct {
	Name      string `json:"name"`
	Path      string `json:"path"` // absolute
	Vars      []Var  `json:"vars"`
	UpdatedAt string `json:"updated_at"`

	keyIndex map[string]int `json:"-"` // lazy, key → index in Vars
}

func (p *Project) buildIndex() {
	if p.keyIndex != nil {
		return
	}
	p.keyIndex = make(map[string]int, len(p.Vars))
	for i, v := range p.Vars {
		p.keyIndex[v.Key] = i
	}
}

// Get returns the value for a key and whether it exists.
func (p *Project) Get(key string) (string, bool) {
	p.buildIndex()
	if i, ok := p.keyIndex[key]; ok {
		return p.Vars[i].Value, true
	}
	return "", false
}

// Set inserts or updates a key, preserving insertion order for existing keys.
func (p *Project) Set(key, value string) {
	p.buildIndex()
	if i, ok := p.keyIndex[key]; ok {
		p.Vars[i].Value = value
		return
	}
	p.keyIndex[key] = len(p.Vars)
	p.Vars = append(p.Vars, Var{Key: key, Value: value})
}

// Unset removes a key. Returns true if it existed.
func (p *Project) Unset(key string) bool {
	p.buildIndex()
	i, ok := p.keyIndex[key]
	if !ok {
		return false
	}
	n := len(p.Vars) - 1
	delete(p.keyIndex, key)
	if i < n {
		// Swap with last to avoid O(n) compaction; update the swapped element's index.
		p.Vars[i] = p.Vars[n]
		p.keyIndex[p.Vars[i].Key] = i
	}
	p.Vars = p.Vars[:n]
	return true
}

// Keys returns a sorted slice of the project's keys (for stable display).
func (p *Project) Keys() []string {
	keys := make([]string, len(p.Vars))
	for i, v := range p.Vars {
		keys[i] = v.Key
	}
	sort.Strings(keys)
	return keys
}

// ParseEnv reads KEY=value lines from .env content. It tolerates:
//   - blank lines and # comments (skipped)
//   - optional "export " prefix
//   - single- or double-quoted values (quotes stripped)
//   - inline values containing '='
//   - multiline quoted values
//   - escape sequences (\n, \t, \\, \", \') inside double-quoted values
func ParseEnv(content string) []Var {
	p := envParser{}
	sc := bufio.NewScanner(strings.NewReader(content))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		p.feed(sc.Text())
	}
	p.flush()
	return p.vars
}

// envParser is a line-by-line state machine that switches between normal
// lines and multiline quoted values.
type envParser struct {
	vars         []Var
	inMultiline  bool
	multilineKey string
	multilineQ   byte
	multilineBuf strings.Builder
}

func (p *envParser) feed(raw string) {
	if p.inMultiline {
		p.feedMultiline(raw)
		return
	}
	p.feedLine(raw)
}

func (p *envParser) feedMultiline(raw string) {
	if p.multilineBuf.Len() > 0 {
		p.multilineBuf.WriteByte('\n')
	}
	if endIdx := strings.IndexByte(raw, p.multilineQ); endIdx >= 0 {
		p.multilineBuf.WriteString(raw[:endIdx])
		p.commitMultiline()
		return
	}
	p.multilineBuf.WriteString(raw)
}

func (p *envParser) feedLine(raw string) {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "#") {
		return
	}
	line = strings.TrimPrefix(line, "export ")
	eq := strings.Index(line, "=")
	if eq < 0 {
		return
	}
	key := strings.TrimSpace(line[:eq])
	val := strings.TrimSpace(line[eq+1:])
	if key == "" {
		return
	}
	if q, ok := startsUnterminatedQuote(val); ok {
		p.multilineKey = key
		p.multilineQ = q
		p.inMultiline = true
		p.multilineBuf.WriteString(val[1:])
		return
	}
	p.vars = append(p.vars, Var{Key: key, Value: unquote(val)})
}

func (p *envParser) commitMultiline() {
	val := p.multilineBuf.String()
	if p.multilineQ == '"' {
		val = unescape(val)
	}
	if p.multilineKey != "" {
		p.vars = append(p.vars, Var{Key: p.multilineKey, Value: val})
	}
	p.multilineKey = ""
	p.multilineQ = 0
	p.inMultiline = false
	p.multilineBuf.Reset()
}

func (p *envParser) flush() {
	if p.inMultiline && p.multilineKey != "" {
		p.commitMultiline()
	}
}

// startsUnterminatedQuote reports whether val begins with a quote that is not
// closed on the same line. When true, the caller should switch to multiline mode.
func startsUnterminatedQuote(val string) (byte, bool) {
	if len(val) < 2 {
		return 0, false
	}
	q := val[0]
	if q != '"' && q != '\'' {
		return 0, false
	}
	if strings.IndexByte(val[1:], q) < 0 {
		return q, true
	}
	return 0, false
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			inner := s[1 : len(s)-1]
			if s[0] == '"' {
				return unescape(inner)
			}
			return inner
		}
	}
	return s
}

func unescape(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			switch c {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case '\\':
				b.WriteByte('\\')
			case '"':
				b.WriteByte('"')
			case '\'':
				b.WriteByte('\'')
			default:
				b.WriteByte('\\')
				b.WriteByte(c)
			}
			escaped = false
		} else if c == '\\' {
			escaped = true
		} else {
			b.WriteByte(c)
		}
	}
	if escaped {
		b.WriteByte('\\')
	}
	return b.String()
}

func escape(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\n':
			b.WriteString("\\n")
		case '\t':
			b.WriteString("\\t")
		case '\r':
			b.WriteString("\\r")
		case '\\':
			b.WriteString("\\\\")
		case '"':
			b.WriteString("\\\"")
		default:
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

func needsQuoting(s string) bool {
	if strings.ContainsAny(s, " #\"'\t") {
		return true
	}
	return strings.ContainsRune(s, '\n')
}

// serialize writes a header followed by KEY=value lines produced by valueFor.
func serialize(header string, vars []Var, valueFor func(Var) string) string {
	var b strings.Builder
	b.WriteString(header)
	for _, v := range vars {
		fmt.Fprintf(&b, "%s=%s\n", v.Key, valueFor(v))
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

// ReadEnvFile reads and parses a .env file.
func ReadEnvFile(path string) ([]Var, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseEnv(string(data)), nil
}

// WriteEnvFile writes variables to disk with owner-only permissions.
func WriteEnvFile(path string, vars []Var) error {
	return os.WriteFile(path, []byte(SerializeEnv(vars)), 0o600)
}

// WriteSafeEnvFile writes with values replaced by SafePlaceholder.
func WriteSafeEnvFile(path string, vars []Var) error {
	return os.WriteFile(path, []byte(SerializeSafeEnv(vars)), 0o600)
}

// SerializeExampleEnv produces .env content with empty values for .env.example files.
func SerializeExampleEnv(vars []Var) string {
	const header = "# Managed by calypso. Example — fill in values and `calypso push` to import.\n"
	return serialize(header, vars, func(Var) string { return "" })
}

// WriteExampleEnvFile writes a .env.example template with empty values.
func WriteExampleEnvFile(path string, vars []Var) error {
	return os.WriteFile(path, []byte(SerializeExampleEnv(vars)), 0o644)
}
