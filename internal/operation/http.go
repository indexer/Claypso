// Package operation executes fixed, owner-configured operations without
// exposing credential names, values, upstream bodies, or transport details to
// the caller.
package operation

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/yemon/calypso/internal/project"
	"github.com/yemon/calypso/internal/vault"
)

var (
	ErrPolicy           = errors.New("trusted operation policy is invalid")
	ErrCredential       = errors.New("trusted operation credential is unavailable")
	ErrTransport        = errors.New("trusted operation transport failed")
	ErrUnexpectedStatus = errors.New("trusted operation returned an unexpected status")
)

const maxDiscardBody = 64 << 10

type Result struct {
	Status   int
	Duration time.Duration
}

// Run performs a fixed request. The HTTP client deliberately ignores proxy
// environment variables, never follows redirects, and discards only a bounded
// amount of response data. Returned errors contain no URL, header, credential,
// response body, DNS detail, or upstream diagnostic.
func Run(ctx context.Context, op *vault.TrustedOperation, env *project.Environment) (Result, error) {
	if err := vault.ValidateTrustedOperation(op); err != nil {
		return Result{}, ErrPolicy
	}
	timeout := time.Duration(op.TimeoutSeconds) * time.Second
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:   true,
		MaxIdleConns:        4,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: min(timeout, 10*time.Second),
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return runWithClient(ctx, op, env, client)
}

func runWithClient(ctx context.Context, op *vault.TrustedOperation, env *project.Environment, client *http.Client) (Result, error) {
	if err := vault.ValidateTrustedOperation(op); err != nil {
		return Result{}, ErrPolicy
	}
	secret, ok := env.Get(op.SecretKey)
	if !ok || secret == "" || !safeHeaderValue(op.SecretPrefix+secret) {
		return Result{}, ErrCredential
	}
	timeout := time.Duration(op.TimeoutSeconds) * time.Second
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(runCtx, op.Method, op.URL, nil)
	if err != nil {
		return Result{}, ErrPolicy
	}
	req.Header.Set(op.SecretHeader, op.SecretPrefix+secret)
	req.Header.Set("User-Agent", "calypso-operation/1")

	started := time.Now()
	resp, err := client.Do(req)
	elapsed := time.Since(started)
	if err != nil {
		return Result{Duration: elapsed}, ErrTransport
	}
	defer resp.Body.Close()
	_, _ = io.CopyN(io.Discard, resp.Body, maxDiscardBody) //nolint:errcheck // bounded drain for connection reuse; body content is deliberately ignored

	result := Result{Status: resp.StatusCode, Duration: elapsed}
	if resp.StatusCode < op.ExpectMin || resp.StatusCode > op.ExpectMax {
		return result, ErrUnexpectedStatus
	}
	return result, nil
}

func safeHeaderValue(value string) bool {
	for _, r := range value {
		if r == '\r' || r == '\n' || r == 0x7f || (r < 0x20 && r != '\t') {
			return false
		}
	}
	return !strings.ContainsRune(value, 0)
}
