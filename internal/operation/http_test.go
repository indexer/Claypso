package operation

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/yemon/calypso/internal/project"
	"github.com/yemon/calypso/internal/vault"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func operationFixture() (*vault.TrustedOperation, *project.Environment) {
	op := &vault.TrustedOperation{
		ID:             "op_0123456789abcdef",
		Label:          "health",
		Spec:           "web",
		Method:         http.MethodGet,
		URL:            "https://service.example.test/health",
		SecretKey:      "TOKEN",
		SecretHeader:   "Authorization",
		SecretPrefix:   "Bearer ",
		ExpectMin:      200,
		ExpectMax:      299,
		TimeoutSeconds: 5,
	}
	env := &project.Environment{Name: project.DefaultEnvName}
	env.Set("TOKEN", "runtime-secret-value")
	return op, env
}

func TestRunWithClientUsesCredentialAndDiscardsBody(t *testing.T) {
	op, env := operationFixture()
	called := false
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		if got := req.Header.Get("Authorization"); got != "Bearer runtime-secret-value" {
			t.Fatalf("credential header mismatch")
		}
		return &http.Response{
			StatusCode: 204,
			Body:       io.NopCloser(strings.NewReader("runtime-secret-value should never be returned")),
			Header:     make(http.Header),
		}, nil
	})}
	result, err := runWithClient(context.Background(), op, env, client)
	if err != nil {
		t.Fatal(err)
	}
	if !called || result.Status != 204 {
		t.Fatalf("operation did not complete: called=%v result=%+v", called, result)
	}
}

func TestRunWithClientReturnsOnlyGenericErrors(t *testing.T) {
	op, env := operationFixture()
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("dial service.example.test with runtime-secret-value")
	})}
	_, err := runWithClient(context.Background(), op, env, client)
	if !errors.Is(err, ErrTransport) {
		t.Fatalf("got %v, want ErrTransport", err)
	}
	for _, forbidden := range []string{"runtime-secret-value", "TOKEN", "service.example.test"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("generic error leaked protected material")
		}
	}
}

func TestRunWithClientUnexpectedStatusHasBoundedResult(t *testing.T) {
	op, env := operationFixture()
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 401,
			Body:       io.NopCloser(strings.NewReader("upstream diagnostic and token")),
			Header:     make(http.Header),
		}, nil
	})}
	result, err := runWithClient(context.Background(), op, env, client)
	if !errors.Is(err, ErrUnexpectedStatus) || result.Status != 401 {
		t.Fatalf("got result=%+v err=%v", result, err)
	}
}

func TestRunWithClientRejectsUnsafeCredentialHeader(t *testing.T) {
	op, env := operationFixture()
	env.Set("TOKEN", "bad\r\nInjected: value")
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatal("transport must not run for an unsafe header value")
		return nil, nil
	})}
	if _, err := runWithClient(context.Background(), op, env, client); !errors.Is(err, ErrCredential) {
		t.Fatalf("got %v, want ErrCredential", err)
	}
}
