package vault

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/yemon/calypso/internal/project"
)

const (
	// TrustedOperation limits keep encrypted policy entries bounded and make
	// validation deterministic before any network request is attempted.
	MaxOperationIDLen     = 80
	MaxOperationLabelLen  = 120
	MaxOperationPrefixLen = 256
	MaxOperationURLLen    = 4096
	MaxOperationTimeout   = 2 * time.Minute
)

// TrustedOperation is an owner-configured, fixed HTTP request. SecretKey is
// intentionally stored only inside the encrypted vault and is never emitted
// by strict-mode list, run, audit, or broker responses.
type TrustedOperation struct {
	ID             string `json:"id"`
	Label          string `json:"label"`
	Spec           string `json:"spec"`
	Method         string `json:"method"`
	URL            string `json:"url"`
	SecretKey      string `json:"secret_key"`
	SecretHeader   string `json:"secret_header"`
	SecretPrefix   string `json:"secret_prefix,omitempty"`
	ExpectMin      int    `json:"expect_min"`
	ExpectMax      int    `json:"expect_max"`
	TimeoutSeconds int    `json:"timeout_seconds"`
	CreatedAt      string `json:"created_at"`
}

// ValidateTrustedOperation rejects dynamic or ambiguous policy. Loopback HTTP
// is allowed for local development and black-box tests; every remote target
// must use HTTPS.
func ValidateTrustedOperation(op *TrustedOperation) error {
	if op == nil {
		return fmt.Errorf("trusted operation is nil")
	}
	if !validOpaqueID(op.ID) {
		return fmt.Errorf("invalid operation ID")
	}
	if label := strings.TrimSpace(op.Label); label == "" || len(label) > MaxOperationLabelLen || hasControl(label) {
		return fmt.Errorf("operation label must be 1..%d characters", MaxOperationLabelLen)
	}
	if _, err := ParseSpec(op.Spec); err != nil {
		return fmt.Errorf("invalid project environment reference: %w", err)
	}
	switch op.Method {
	case http.MethodGet, http.MethodHead, http.MethodPost:
	default:
		return fmt.Errorf("method must be GET, HEAD, or POST")
	}
	if len(op.URL) > MaxOperationURLLen {
		return fmt.Errorf("operation URL exceeds %d bytes", MaxOperationURLLen)
	}
	u, err := url.Parse(op.URL)
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("operation URL must be an absolute URL without credentials or a fragment")
	}
	httpLoopback := u.Scheme == "http" && loopbackHost(u.Hostname())
	if u.Scheme != "https" && !httpLoopback {
		return fmt.Errorf("remote operation URLs must use https (http is allowed only for loopback)")
	}
	if !validHeaderName(op.SecretHeader) {
		return fmt.Errorf("invalid secret header name")
	}
	switch strings.ToLower(op.SecretHeader) {
	case "host", "content-length", "transfer-encoding", "connection", "proxy-authorization", "proxy-authenticate", "trailer", "upgrade":
		return fmt.Errorf("secret header cannot be a routing or hop-by-hop header")
	}
	if !validHeaderValue(op.SecretPrefix) || len(op.SecretPrefix) > MaxOperationPrefixLen {
		return fmt.Errorf("secret prefix is invalid or exceeds %d bytes", MaxOperationPrefixLen)
	}
	if !project.ValidKey(op.SecretKey) {
		return fmt.Errorf("secret key is invalid")
	}
	if op.ExpectMin < 100 || op.ExpectMax > 599 || op.ExpectMin > op.ExpectMax {
		return fmt.Errorf("expected status range must be within 100..599")
	}
	timeout := time.Duration(op.TimeoutSeconds) * time.Second
	if timeout <= 0 || timeout > MaxOperationTimeout {
		return fmt.Errorf("timeout must be between 1s and %s", MaxOperationTimeout)
	}
	return nil
}

func validOpaqueID(id string) bool {
	if id == "" || len(id) > MaxOperationIDLen {
		return false
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

// ValidTrustedOperationID is safe to use at untrusted broker/CLI boundaries.
func ValidTrustedOperationID(id string) bool { return validOpaqueID(id) }

func loopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("!#$%&'*+-.^_`|~", r):
		default:
			return false
		}
	}
	return true
}

func validHeaderValue(value string) bool {
	for _, r := range value {
		if r == '\r' || r == '\n' || r == 0x7f || (r < 0x20 && r != '\t') {
			return false
		}
	}
	return true
}

func hasControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

func (v *Vault) AddTrustedOperation(op *TrustedOperation) error {
	if err := ValidateTrustedOperation(op); err != nil {
		return err
	}
	if v.TrustedOperations == nil {
		v.TrustedOperations = make(map[string]*TrustedOperation)
	}
	if _, exists := v.TrustedOperations[op.ID]; exists {
		return fmt.Errorf("trusted operation already exists")
	}
	_, env, err := v.ResolveEnv(op.Spec)
	if err != nil {
		return err
	}
	if _, ok := env.Get(op.SecretKey); !ok {
		return fmt.Errorf("configured secret key was not found in the selected environment")
	}
	copyOp := *op
	v.TrustedOperations[op.ID] = &copyOp
	return nil
}

func (v *Vault) RemoveTrustedOperation(id string) error {
	if _, ok := v.TrustedOperations[id]; !ok {
		return fmt.Errorf("trusted operation not found")
	}
	delete(v.TrustedOperations, id)
	return nil
}

func (v *Vault) TrustedOperation(id string) (*TrustedOperation, error) {
	op, ok := v.TrustedOperations[id]
	if !ok || op == nil {
		return nil, fmt.Errorf("trusted operation not found")
	}
	if err := ValidateTrustedOperation(op); err != nil {
		return nil, fmt.Errorf("trusted operation policy is invalid")
	}
	return op, nil
}

func (v *Vault) TrustedOperationIDs() []string {
	ids := make([]string, 0, len(v.TrustedOperations))
	for id := range v.TrustedOperations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
