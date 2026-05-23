// Package project models a registered project and its .env variables across
// one or more environments (dev, staging, production, etc.).
package project

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
)

const SafePlaceholder = "****"

// DefaultEnvName is the env created when a project is added without an
// explicit env name. Loading a v1 vault also migrates each project's flat
// vars into an Environment under this name.
const DefaultEnvName = "default"

// Var is a single environment variable.
type Var struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Environment is one named variant of a project (dev / staging / prod /
// default). It holds the .env path for that variant and its variables.
type Environment struct {
	Name      string `json:"name"`
	Path      string `json:"path"` // absolute
	Vars      []Var  `json:"vars"`
	UpdatedAt string `json:"updated_at"`

	keyIndex map[string]int `json:"-"` // lazy, key → index in Vars
}

// InvalidateIndex must be called by any caller that replaces or reorders
// e.Vars directly (e.g. push imports). Set/Unset keep the index in sync
// automatically.
func (e *Environment) InvalidateIndex() {
	e.keyIndex = nil
}

func (e *Environment) buildIndex() {
	if e.keyIndex != nil {
		return
	}
	e.keyIndex = make(map[string]int, len(e.Vars))
	for i, v := range e.Vars {
		e.keyIndex[v.Key] = i
	}
}

// Get returns the value for a key and whether it exists.
func (e *Environment) Get(key string) (string, bool) {
	e.buildIndex()
	if i, ok := e.keyIndex[key]; ok {
		return e.Vars[i].Value, true
	}
	return "", false
}

// Set inserts or updates a key, preserving insertion order for existing keys.
func (e *Environment) Set(key, value string) {
	e.buildIndex()
	if i, ok := e.keyIndex[key]; ok {
		e.Vars[i].Value = value
		return
	}
	e.keyIndex[key] = len(e.Vars)
	e.Vars = append(e.Vars, Var{Key: key, Value: value})
}

// Unset removes a key. Returns true if it existed.
func (e *Environment) Unset(key string) bool {
	e.buildIndex()
	i, ok := e.keyIndex[key]
	if !ok {
		return false
	}
	n := len(e.Vars) - 1
	delete(e.keyIndex, key)
	if i < n {
		e.Vars[i] = e.Vars[n]
		e.keyIndex[e.Vars[i].Key] = i
	}
	e.Vars = e.Vars[:n]
	return true
}

// Keys returns a sorted slice of the env's keys (for stable display).
func (e *Environment) Keys() []string {
	keys := make([]string, len(e.Vars))
	for i, v := range e.Vars {
		keys[i] = v.Key
	}
	sort.Strings(keys)
	return keys
}

// Project is a registered project with one or more named environments.
type Project struct {
	Name      string                  `json:"name"`
	Envs      map[string]*Environment `json:"envs"`
	UpdatedAt string                  `json:"updated_at"`
}

// Env fetches an environment by name.
func (p *Project) Env(name string) (*Environment, bool) {
	e, ok := p.Envs[name]
	return e, ok
}

// EnvNames returns environment names sorted alphabetically.
func (p *Project) EnvNames() []string {
	names := make([]string, 0, len(p.Envs))
	for n := range p.Envs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// SoleEnv returns the project's only environment if it has exactly one.
// Used to resolve bare `myapp` references when no @env was supplied.
func (p *Project) SoleEnv() (*Environment, bool) {
	if len(p.Envs) != 1 {
		return nil, false
	}
	for _, e := range p.Envs {
		return e, true
	}
	return nil, false
}

// AddEnv attaches a new environment. Errors if one with that name already exists.
func (p *Project) AddEnv(name, absPath, stamp string) (*Environment, error) {
	if p.Envs == nil {
		p.Envs = make(map[string]*Environment)
	}
	if _, exists := p.Envs[name]; exists {
		return nil, fmt.Errorf("environment %q already exists for project %q", name, p.Name)
	}
	e := &Environment{
		Name:      name,
		Path:      absPath,
		UpdatedAt: stamp,
	}
	p.Envs[name] = e
	return e, nil
}

// RemoveEnv detaches an environment. Errors if it's the project's last env;
// callers should use Vault.RemoveProject in that case.
func (p *Project) RemoveEnv(name string) error {
	if _, ok := p.Envs[name]; !ok {
		return fmt.Errorf("environment %q not found in project %q", name, p.Name)
	}
	if len(p.Envs) == 1 {
		return fmt.Errorf("cannot remove last environment of project %q; remove the project instead", p.Name)
	}
	delete(p.Envs, name)
	return nil
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
