package dashboard

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/yemon/calypso/internal/analysis"
	"github.com/yemon/calypso/internal/vault"
)

// Compile-time guarantee that the concrete vault used in these tests still
// satisfies the read-only surface the dashboard depends on. If the interface
// drifts, this fails to build rather than at runtime.
var (
	_ VaultReader          = (*vault.Vault)(nil)
	_ analysis.VaultReader = (*vault.Vault)(nil)
)

// populatedVault builds a fixture exercising every dashboard section:
//   - multiple projects, one with two envs (prod, staging)
//   - a cross-project gap: alpha@prod and beta@prod both have DB_HOST/DB_PORT,
//     but beta@prod is missing API_KEY (defined in alpha@prod and gamma@prod).
//   - an intra-project gap: alpha@staging lacks API_KEY that alpha@prod has.
//   - a non-empty key matrix.
func populatedVault(t *testing.T) *vault.Vault {
	t.Helper()
	v := vault.New()

	alpha, ap, err := v.AddProject("alpha", "prod", "/srv/alpha/prod.env")
	if err != nil {
		t.Fatalf("AddProject alpha/prod: %v", err)
	}
	ap.Set("DB_HOST", "localhost")
	ap.Set("DB_PORT", "5432")
	ap.Set("API_KEY", "sk-alpha")

	// Second env on the same project, missing API_KEY -> intra-project gap.
	as, err := alpha.AddEnv("staging", "/srv/alpha/staging.env", "2026-05-01")
	if err != nil {
		t.Fatalf("AddEnv alpha/staging: %v", err)
	}
	as.Set("DB_HOST", "stage-db")
	as.Set("DB_PORT", "5432")

	_, bp, err := v.AddProject("beta", "prod", "/srv/beta/prod.env")
	if err != nil {
		t.Fatalf("AddProject beta/prod: %v", err)
	}
	bp.Set("DB_HOST", "localhost")
	bp.Set("DB_PORT", "3306")
	// beta@prod intentionally has no API_KEY -> cross-project gap.

	_, gp, err := v.AddProject("gamma", "prod", "/srv/gamma/prod.env")
	if err != nil {
		t.Fatalf("AddProject gamma/prod: %v", err)
	}
	gp.Set("DB_HOST", "localhost")
	gp.Set("API_KEY", "sk-gamma")

	return v
}

// doRequest spins up the dashboard handler in-process (no real port bound)
// and returns the recorded response for GET path.
func doRequest(t *testing.T, v VaultReader, path string) *httptest.ResponseRecorder {
	t.Helper()
	h := newHandler(v)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestNewHandler_RendersPopulatedVault(t *testing.T) {
	v := populatedVault(t)
	rec := doRequest(t, v, "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	body := rec.Body.String()

	// Skeleton + header.
	for _, want := range []string{
		"<!doctype html>",
		"<title>calypso</title>",
		"values are never shown",
		"Projects",
		"Cross-project gaps",
		"Intra-project gaps",
		"Key matrix",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing section/marker %q", want)
		}
	}

	// Every project name should appear.
	for _, name := range []string{"alpha", "beta", "gamma"} {
		if !strings.Contains(body, name) {
			t.Errorf("body missing project %q", name)
		}
	}

	// Env tags + path-mono cells.
	for _, want := range []string{
		`<span class="env-tag">prod</span>`,
		`<span class="env-tag">staging</span>`,
		"/srv/alpha/prod.env",
		"/srv/alpha/staging.env",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing env detail %q", want)
		}
	}

	// Matrix keys are rendered as key pills.
	for _, key := range []string{"DB_HOST", "DB_PORT", "API_KEY"} {
		if !strings.Contains(body, `<span class="pill key">`+key+`</span>`) {
			t.Errorf("body missing matrix key pill for %q", key)
		}
	}

	// Gaps populated -> warn badges present, not the all-clear text.
	if !strings.Contains(body, `class="badge warn"`) {
		t.Errorf("expected a warn badge for populated gaps")
	}
	if strings.Contains(body, "All shared keys are present") {
		t.Errorf("cross-project all-clear text shown despite a real gap")
	}
	if strings.Contains(body, "Every env of every project has the same keys") {
		t.Errorf("intra-project all-clear text shown despite a real gap")
	}
}

func TestNewHandler_CrossProjectGapContent(t *testing.T) {
	v := populatedVault(t)
	body := doRequest(t, v, "/").Body.String()

	// beta@prod is missing API_KEY which alpha@prod and gamma@prod define.
	// Cross-project gaps render the missing ref label "beta" (default-env
	// labels collapse, but these are @prod) and the key pill.
	if !strings.Contains(body, "API_KEY") {
		t.Fatalf("API_KEY gap key not rendered")
	}
	if !strings.Contains(body, "beta@prod") {
		t.Errorf("expected cross-project gap to reference beta@prod; body:\n%s", body)
	}
}

func TestNewHandler_SecurityHeaders(t *testing.T) {
	v := populatedVault(t)
	rec := doRequest(t, v, "/")

	checks := map[string]string{
		"Content-Security-Policy": "default-src 'self'; style-src 'unsafe-inline'",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
	}
	for header, want := range checks {
		if got := rec.Header().Get(header); got != want {
			t.Errorf("header %s = %q, want %q", header, got, want)
		}
	}
}

func TestNewHandler_EmptyVault(t *testing.T) {
	v := vault.New() // no projects at all
	rec := doRequest(t, v, "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()

	// Page still renders fully.
	if !strings.Contains(body, "<title>calypso</title>") {
		t.Errorf("empty vault did not render page skeleton")
	}

	// Empty-state copy for every collection-driven section.
	wantEmptyStates := []string{
		"All shared keys are present in every project's matching env.",
		"Every env of every project has the same keys as its siblings.",
		"No keys stored yet.",
	}
	for _, want := range wantEmptyStates {
		if !strings.Contains(body, want) {
			t.Errorf("empty vault missing empty-state copy %q", want)
		}
	}

	// Empty gap sections show the "ok" badge, never a warn badge.
	if !strings.Contains(body, `class="badge ok"`) {
		t.Errorf("empty vault should show ok badges")
	}
	if strings.Contains(body, `class="badge warn"`) {
		t.Errorf("empty vault should not show any warn badge")
	}

	// Project count badge should read zero. Pin the Projects-specific
	// fragment: a bare `<span class="badge">0</span>` also matches the Key
	// matrix badge (both are 0 for an empty vault), so include the "Projects"
	// label that immediately precedes it in page.html.
	if !strings.Contains(body, `Projects <span class="badge">0</span>`) {
		t.Errorf("empty vault should show a zero Projects count badge")
	}
}

func TestNewHandler_NoGapsButHasKeys(t *testing.T) {
	// Single project, single env: there are keys (matrix populated) but no
	// sibling envs to drift against, so both gap sections are clear.
	v := vault.New()
	_, p, err := v.AddProject("solo", "prod", "/srv/solo/.env")
	if err != nil {
		t.Fatalf("AddProject: %v", err)
	}
	p.Set("ONLY_KEY", "x")
	p.Set("ANOTHER", "y")

	body := doRequest(t, v, "/").Body.String()

	if !strings.Contains(body, "All shared keys are present") {
		t.Errorf("expected cross-project all-clear with a single project")
	}
	if !strings.Contains(body, "Every env of every project has the same keys") {
		t.Errorf("expected intra-project all-clear with a single env")
	}
	// Matrix is non-empty.
	if strings.Contains(body, "No keys stored yet.") {
		t.Errorf("matrix should not be empty for a project with keys")
	}
	for _, key := range []string{"ONLY_KEY", "ANOTHER"} {
		if !strings.Contains(body, `<span class="pill key">`+key+`</span>`) {
			t.Errorf("matrix missing key %q", key)
		}
	}
}

func TestNewHandler_OverHTTPServer(t *testing.T) {
	// End-to-end through a real (ephemeral-port) test server, never binding a
	// fixed port.
	v := populatedVault(t)
	ts := httptest.NewServer(newHandler(v))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(b), "<title>calypso</title>") {
		t.Errorf("served body missing page title")
	}
	if got := resp.Header.Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
}

func TestBuildView_ShapeMatchesVault(t *testing.T) {
	v := populatedVault(t)
	view := buildView(v)

	if len(view.Projects) != 3 {
		t.Errorf("Projects = %d, want 3", len(view.Projects))
	}

	// alpha has two envs; locate it and assert env count + var counts.
	var alpha *projectView
	for i := range view.Projects {
		if view.Projects[i].Name == "alpha" {
			alpha = &view.Projects[i]
		}
	}
	if alpha == nil {
		t.Fatalf("alpha project not present in view")
	}
	if len(alpha.Envs) != 2 {
		t.Fatalf("alpha envs = %d, want 2 (prod, staging)", len(alpha.Envs))
	}
	// EnvNames are sorted: prod then staging.
	var prodVarCount int
	for _, e := range alpha.Envs {
		if e.Name == "prod" {
			prodVarCount = e.VarCount
		}
	}
	if prodVarCount != 3 {
		t.Errorf("alpha@prod VarCount = %d, want 3", prodVarCount)
	}

	if len(view.CrossProjectGaps) == 0 {
		t.Errorf("expected at least one cross-project gap")
	}
	if len(view.IntraProjectGaps) == 0 {
		t.Errorf("expected at least one intra-project gap")
	}
	if len(view.Matrix) == 0 {
		t.Errorf("expected a non-empty key matrix")
	}
}
