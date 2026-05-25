// Package dashboard serves a read-only web overview of the vault on localhost.
// Values are never shown in the dashboard.
package dashboard

import (
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yemon/calypso/internal/analysis"
	"github.com/yemon/calypso/internal/project"
)

//go:embed page.html
var pageHTML string

// VaultReader is the read-only surface this package needs. Matches
// analysis.VaultReader so the same vault works for both.
type VaultReader interface {
	Names() []string
	Project(name string) (*project.Project, error)
	ResolveEnv(spec string) (*project.Project, *project.Environment, error)
}

type viewData struct {
	Projects         []projectView
	CrossProjectGaps []analysis.Gap
	IntraProjectGaps []analysis.Gap
	Matrix           []matrixRow
}

type projectView struct {
	Name      string
	UpdatedAt string
	Envs      []envView
}

type envView struct {
	Name      string
	Path      string
	VarCount  int
	UpdatedAt string
}

// matrixRow is one row of the key matrix, pre-formatted into env labels
// (e.g. "myapp@prod") so the template doesn't need to know about EnvRef.
type matrixRow struct {
	Key  string
	Refs []string
}

var pageTmpl = template.Must(template.New("page").Parse(pageHTML))

// Serve starts the dashboard on 127.0.0.1:<port> and blocks until SIGINT or
// SIGTERM. The vault is passed already-decrypted; the server holds it in
// memory only.
func Serve(v VaultReader, port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("dashboard cannot bind localhost:%d: %w", port, err)
	}
	fmt.Printf("calypso dashboard → http://127.0.0.1:%d  (Ctrl+C to stop)\n", port)
	srv := &http.Server{Handler: newHandler(v)}
	return serveUntilSignal(srv, ln)
}

func newHandler(v VaultReader) http.Handler {
	view := buildView(v)

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'unsafe-inline'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		if err := pageTmpl.Execute(w, view); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})
	return mux
}

// serveUntilSignal runs the server until SIGINT or SIGTERM, then shuts down gracefully.
func serveUntilSignal(srv *http.Server, ln net.Listener) error {
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case <-sig:
		fmt.Fprintln(os.Stderr, "\nShutting down dashboard...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}

func buildView(v VaultReader) viewData {
	names := v.Names()
	pvs := make([]projectView, 0, len(names))
	for _, name := range names {
		p, err := v.Project(name)
		if err != nil {
			continue
		}
		envs := make([]envView, 0, len(p.Envs))
		for _, en := range p.EnvNames() {
			e := p.Envs[en]
			envs = append(envs, envView{
				Name: en, Path: e.Path,
				VarCount: len(e.Vars), UpdatedAt: e.UpdatedAt,
			})
		}
		pvs = append(pvs, projectView{
			Name: p.Name, UpdatedAt: p.UpdatedAt, Envs: envs,
		})
	}

	matrix := analysis.KeyMatrix(v)
	rows := make([]matrixRow, 0, len(matrix))
	for _, ku := range matrix {
		labels := make([]string, len(ku.Refs))
		for i, r := range ku.Refs {
			labels[i] = r.String()
		}
		rows = append(rows, matrixRow{Key: ku.Key, Refs: labels})
	}

	return viewData{
		Projects:         pvs,
		CrossProjectGaps: analysis.FindCrossProjectGaps(v),
		IntraProjectGaps: analysis.FindIntraProjectGaps(v),
		Matrix:           rows,
	}
}
