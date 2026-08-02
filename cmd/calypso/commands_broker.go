package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	runtimeop "github.com/yemon/calypso/internal/operation"
	"github.com/yemon/calypso/internal/vault"
)

const maxBrokerResponse = 32 << 10

type brokerConnection struct {
	Endpoint string `json:"endpoint"`
	Token    string `json:"token"`
}

type brokerResponse struct {
	OK          bool   `json:"ok"`
	OperationID string `json:"operation_id,omitempty"`
	Status      int    `json:"status,omitempty"`
	DurationMS  int64  `json:"duration_ms,omitempty"`
	Error       string `json:"error,omitempty"`
}

func brokerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "broker",
		Short: "Serve or invoke constrained trusted operations",
		Long: `The broker keeps vault credentials inside a hardened Calypso process.
It exposes only fixed trusted operations over a loopback capability endpoint.

For the strongest boundary, run 'broker serve' under an OS identity that can
unlock the vault while the coding-agent identity can access only the generated
connection capability and 'broker invoke'.`,
	}
	cmd.AddCommand(brokerServeCmd(), brokerInvokeCmd())
	return cmd
}

func brokerServeCmd() *cobra.Command {
	var listenAddr, connectionFile, connectionMode string
	var allowedIDs []string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve trusted operations on a loopback capability endpoint",
		RunE: func(cmd *cobra.Command, args []string) error {
			if connectionFile == "" {
				return fmt.Errorf("--connection-file is required")
			}
			if err := validateLoopbackListen(listenAddr); err != nil {
				return err
			}

			v, pw, err := openOperationVault(cmd.Context(), "broker serve")
			if err != nil {
				return err
			}
			clearBytes(pw)
			defer v.Close()
			if !v.AgentStrict {
				return fmt.Errorf("broker serve requires agent strict mode; configure operations, then run `calypso strict on`")
			}
			if len(allowedIDs) == 0 {
				return fmt.Errorf("broker serve requires at least one --allow operation ID")
			}
			allowed := make(map[string]bool, len(allowedIDs))
			for _, id := range allowedIDs {
				if _, err := v.TrustedOperation(id); err != nil {
					return fmt.Errorf("broker allowlist contains an unknown or invalid operation")
				}
				allowed[id] = true
			}

			ln, err := net.Listen("tcp", listenAddr)
			if err != nil {
				return fmt.Errorf("broker listen failed")
			}
			defer ln.Close()
			if tcp, ok := ln.Addr().(*net.TCPAddr); !ok || !tcp.IP.IsLoopback() {
				return fmt.Errorf("broker refused a non-loopback listener")
			}

			token, err := newBrokerToken()
			if err != nil {
				return err
			}
			conn := brokerConnection{
				Endpoint: "http://" + ln.Addr().String(),
				Token:    token,
			}
			mode, err := parseConnectionMode(connectionMode)
			if err != nil {
				return err
			}
			if err := writeBrokerConnection(connectionFile, conn, mode); err != nil {
				return err
			}
			defer os.Remove(connectionFile)

			handler := &brokerHandler{vault: v, token: token, allowed: allowed}
			server := &http.Server{
				Handler:           handler,
				ReadHeaderTimeout: 5 * time.Second,
				IdleTimeout:       30 * time.Second,
				MaxHeaderBytes:    8 << 10,
				ErrorLog:          log.New(io.Discard, "", 0),
			}

			runCtx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			serveErr := make(chan error, 1)
			go func() {
				err := server.Serve(ln)
				if err == http.ErrServerClosed {
					err = nil
				}
				serveErr <- err
			}()

			fmt.Printf("Broker listening on %s; capability written to %s.\n", conn.Endpoint, connectionFile)
			select {
			case err := <-serveErr:
				if err != nil {
					return fmt.Errorf("broker stopped unexpectedly")
				}
				return nil
			case <-runCtx.Done():
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = server.Shutdown(shutdownCtx) //nolint:errcheck // best-effort drain; the Serve error below is the one that matters
				return <-serveErr
			}
		},
	}
	cmd.Flags().StringVar(&listenAddr, "listen", "127.0.0.1:0", "loopback listen address")
	cmd.Flags().StringVar(&connectionFile, "connection-file", "", "JSON capability file for broker clients")
	cmd.Flags().StringVar(&connectionMode, "connection-mode", "0600", "capability permissions: 0600 or intentional group-readable 0640")
	cmd.Flags().StringSliceVar(&allowedIDs, "allow", nil, "operation ID this broker capability may invoke (repeatable; required)")
	return cmd
}

type brokerHandler struct {
	vault   *vault.Vault
	token   string
	allowed map[string]bool
	mu      sync.Mutex
}

func (h *brokerHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if !validBearer(r.Header.Get("Authorization"), h.token) {
		writeBrokerJSON(w, http.StatusUnauthorized, brokerResponse{OK: false, Error: "unauthorized"})
		return
	}
	if r.Method != http.MethodPost {
		writeBrokerJSON(w, http.StatusMethodNotAllowed, brokerResponse{OK: false, Error: "method not allowed"})
		return
	}
	const prefix = "/v1/operations/"
	if !strings.HasPrefix(r.URL.Path, prefix) || strings.Contains(strings.TrimPrefix(r.URL.Path, prefix), "/") {
		writeBrokerJSON(w, http.StatusNotFound, brokerResponse{OK: false, Error: "operation not found"})
		return
	}
	id := strings.TrimPrefix(r.URL.Path, prefix)
	if !h.allowed[id] {
		writeBrokerJSON(w, http.StatusNotFound, brokerResponse{OK: false, Error: "operation not found"})
		return
	}
	bodyProbe, _ := io.ReadAll(io.LimitReader(r.Body, 1)) //nolint:errcheck // probe only: any readable byte means a body was sent
	if r.URL.RawQuery != "" || r.ContentLength > 0 || len(bodyProbe) > 0 {
		writeBrokerJSON(w, http.StatusBadRequest, brokerResponse{OK: false, Error: "operation arguments are not accepted"})
		return
	}

	// Serialize requests so one capability cannot create unbounded concurrent
	// use of the same credential or stress memguard-backed values.
	h.mu.Lock()
	defer h.mu.Unlock()

	op, err := h.vault.TrustedOperation(id)
	if err != nil {
		recordAudit("broker-run", id, 0, err)
		writeBrokerJSON(w, http.StatusNotFound, brokerResponse{OK: false, Error: "operation not found"})
		return
	}
	_, env, err := h.vault.ResolveEnv(op.Spec)
	if err != nil {
		recordAudit("broker-run", id, 0, err)
		writeBrokerJSON(w, http.StatusServiceUnavailable, brokerResponse{OK: false, Error: "operation unavailable"})
		return
	}
	result, runErr := runtimeop.Run(r.Context(), op, env)
	recordAudit("broker-run", id, 1, runErr)
	if runErr != nil {
		writeBrokerJSON(w, http.StatusBadGateway, brokerResponse{
			OK:          false,
			OperationID: id,
			Status:      result.Status,
			DurationMS:  result.Duration.Milliseconds(),
			Error:       "operation failed",
		})
		return
	}
	writeBrokerJSON(w, http.StatusOK, brokerResponse{
		OK:          true,
		OperationID: id,
		Status:      result.Status,
		DurationMS:  result.Duration.Milliseconds(),
	})
}

func brokerInvokeCmd() *cobra.Command {
	var connectionFile string
	cmd := &cobra.Command{
		Use:   "invoke <operation-id>",
		Short: "Invoke a broker capability without access to the vault",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if connectionFile == "" {
				return fmt.Errorf("--connection-file is required")
			}
			if !vault.ValidTrustedOperationID(args[0]) {
				return fmt.Errorf("invalid operation ID")
			}
			conn, err := readBrokerConnection(connectionFile)
			if err != nil {
				return err
			}
			req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost,
				conn.Endpoint+"/v1/operations/"+args[0], nil)
			if err != nil {
				return fmt.Errorf("invalid broker connection")
			}
			req.Header.Set("Authorization", "Bearer "+conn.Token)
			client := &http.Client{
				Timeout: 2 * time.Minute,
				Transport: &http.Transport{
					Proxy:       nil,
					DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
				},
			}
			resp, err := client.Do(req)
			if err != nil {
				return fmt.Errorf("broker invocation failed")
			}
			defer resp.Body.Close()
			raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBrokerResponse+1))
			if err != nil || len(raw) > maxBrokerResponse {
				return fmt.Errorf("broker returned an invalid response")
			}
			var result brokerResponse
			if err := json.Unmarshal(raw, &result); err != nil {
				return fmt.Errorf("broker returned an invalid response")
			}
			if resp.StatusCode != http.StatusOK || !result.OK {
				return fmt.Errorf("broker operation failed")
			}
			if result.OperationID != args[0] || result.Status < 100 || result.Status > 599 ||
				result.DurationMS < 0 || result.DurationMS > int64((2*time.Minute)/time.Millisecond) {
				return fmt.Errorf("broker returned an invalid response")
			}
			fmt.Printf("Operation %s succeeded (HTTP %d, %d ms).\n",
				args[0], result.Status, result.DurationMS)
			return nil
		},
	}
	cmd.Flags().StringVar(&connectionFile, "connection-file", "", "broker connection capability JSON")
	return cmd
}

func validateLoopbackListen(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("--listen must be a host:port pair")
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("--listen must use an explicit loopback IP")
	}
	return nil
}

func newBrokerToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate broker capability: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func validBearer(header, token string) bool {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	got := strings.TrimPrefix(header, prefix)
	return len(got) == len(token) && subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1
}

func writeBrokerConnection(path string, conn brokerConnection, mode os.FileMode) error {
	payload, err := json.Marshal(conn)
	if err != nil {
		return err
	}
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return fmt.Errorf("create connection directory: %w", err)
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("broker connection file already exists")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect broker connection path: %w", err)
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return fmt.Errorf("write broker connection: %w", err)
	}
	if _, err := f.Write(append(payload, '\n')); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("write broker connection: %w", err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("sync broker connection: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close broker connection: %w", err)
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("secure broker connection: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("publish broker connection: %w", err)
	}
	return nil
}

func parseConnectionMode(value string) (os.FileMode, error) {
	raw, err := strconv.ParseUint(value, 8, 32)
	if err != nil || (raw != 0o600 && raw != 0o640) {
		return 0, fmt.Errorf("--connection-mode must be 0600 or 0640")
	}
	return os.FileMode(raw), nil
}

func readBrokerConnection(path string) (brokerConnection, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return brokerConnection{}, fmt.Errorf("read broker connection: %w", err)
	}
	var conn brokerConnection
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&conn); err != nil {
		return brokerConnection{}, fmt.Errorf("broker connection is invalid")
	}
	u := strings.TrimPrefix(conn.Endpoint, "http://")
	if u == conn.Endpoint || strings.ContainsAny(u, "/?#") {
		return brokerConnection{}, fmt.Errorf("broker connection is invalid")
	}
	if err := validateLoopbackListen(u); err != nil {
		return brokerConnection{}, fmt.Errorf("broker connection is invalid")
	}
	if len(conn.Token) < 32 || len(conn.Token) > 128 {
		return brokerConnection{}, fmt.Errorf("broker connection is invalid")
	}
	return conn, nil
}

func writeBrokerJSON(w http.ResponseWriter, status int, response brokerResponse) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(response) //nolint:errcheck // client gone mid-write; nothing to report to
}
