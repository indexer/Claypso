package project

import (
	"bufio"
	"strings"
)

// ParseEnv reads KEY=value lines from .env content. It tolerates:
//   - blank lines and # comments (skipped)
//   - optional "export " prefix
//   - single- or double-quoted values (quotes stripped)
//   - inline values containing '='
//   - multiline quoted values
//   - escape sequences (\n, \t, \\, \", \') inside double-quoted values
func ParseEnv(content string) []Var {
	// Strip a leading UTF-8 BOM so an editor that prepends one doesn't fold it
	// into the first key — U+FEFF is not whitespace, so TrimSpace won't drop it.
	content = strings.TrimPrefix(content, "\ufeff")

	p := envParser{}
	sc := bufio.NewScanner(strings.NewReader(content))
	// content is already fully in memory, so cap the scanner's max token at the
	// whole input: a single line can never exceed it, which removes the silent
	// truncation the default 64KB cap would cause. ParseEnv returns no error,
	// so a dropped bufio.ErrTooLong would otherwise be invisible.
	sc.Buffer(make([]byte, 0, 64*1024), len(content)+1)
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
	if strings.ContainsRune(s, '\n') {
		return true
	}
	// A value with leading or trailing whitespace that the parser's
	// Unicode-aware TrimSpace would strip (e.g. NBSP U+00A0, ideographic
	// space U+3000) must be quoted to survive a write→read roundtrip.
	// ASCII space/tab boundaries are already caught above.
	return strings.TrimSpace(s) != s
}
