package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	runtimeop "github.com/yemon/calypso/internal/operation"
	"github.com/yemon/calypso/internal/vault"
)

func operationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "operation",
		Short: "Manage and run fixed credential-broker operations",
		Long: `Trusted operations bind one encrypted credential to a fixed HTTP
request. Agents invoke only an opaque operation ID; they cannot supply a URL,
header, secret key, request body, or arbitrary command.

Add and remove are owner operations and are blocked by agent strict mode.
List and run remain available while strict mode is active.`,
	}
	cmd.AddCommand(operationAddHTTPCmd(), operationRemoveCmd(), operationListCmd(), operationRunCmd())
	return cmd
}

func operationAddHTTPCmd() *cobra.Command {
	var targetURL, key, method, header, prefix, expect string
	var timeout time.Duration
	cmd := &cobra.Command{
		Use:   "add-http <label> <project[@env]>",
		Short: "Create a fixed authenticated HTTP operation",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if targetURL == "" || key == "" {
				return fmt.Errorf("--url and --key are required")
			}
			expectMin, expectMax, err := parseExpectedStatus(expect)
			if err != nil {
				return err
			}
			if timeout%time.Second != 0 {
				return fmt.Errorf("--timeout must be a whole number of seconds")
			}
			id, err := newOperationID()
			if err != nil {
				return err
			}
			op := &vault.TrustedOperation{
				ID:             id,
				Label:          args[0],
				Spec:           args[1],
				Method:         strings.ToUpper(method),
				URL:            targetURL,
				SecretKey:      key,
				SecretHeader:   header,
				SecretPrefix:   prefix,
				ExpectMin:      expectMin,
				ExpectMax:      expectMax,
				TimeoutSeconds: int(timeout / time.Second),
				CreatedAt:      time.Now().UTC().Format(time.RFC3339),
			}

			ctx := cmd.Context()
			v, pw, err := openOwnerVault(ctx, "operation add-http")
			if err != nil {
				return err
			}
			defer clearBytes(pw)
			if err := v.AddTrustedOperation(op); err != nil {
				return err
			}
			if err := saveAndWarn(ctx, v, pw); err != nil {
				return err
			}
			recordAudit("operation-add", id, 0, nil)
			fmt.Printf("Added trusted operation %s (%s).\n", id, op.Label)
			return nil
		},
	}
	cmd.Flags().StringVar(&targetURL, "url", "", "fixed HTTPS endpoint (loopback HTTP is allowed)")
	cmd.Flags().StringVar(&key, "key", "", "vault key bound to the request (owner configuration only)")
	cmd.Flags().StringVar(&method, "method", "GET", "fixed HTTP method: GET, HEAD, or POST")
	cmd.Flags().StringVar(&header, "header", "Authorization", "fixed request header carrying the credential")
	cmd.Flags().StringVar(&prefix, "prefix", "Bearer ", "non-secret prefix placed before the credential")
	cmd.Flags().StringVar(&expect, "expect", "200-299", "accepted HTTP status or inclusive range")
	cmd.Flags().DurationVar(&timeout, "timeout", 15*time.Second, "request timeout (max 2m)")
	return cmd
}

func operationRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove <operation-id>",
		Short: "Remove a trusted operation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			v, pw, err := openOwnerVault(ctx, "operation remove")
			if err != nil {
				return err
			}
			defer clearBytes(pw)
			if err := v.RemoveTrustedOperation(args[0]); err != nil {
				return err
			}
			if err := saveAndWarn(ctx, v, pw); err != nil {
				return err
			}
			recordAudit("operation-remove", args[0], 0, nil)
			fmt.Printf("Removed trusted operation %s.\n", args[0])
			return nil
		},
	}
}

func operationListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List opaque trusted-operation capabilities",
		RunE: func(cmd *cobra.Command, args []string) error {
			v, pw, err := openOperationVault(cmd.Context(), "operation list")
			if err != nil {
				return err
			}
			clearBytes(pw)
			if len(v.TrustedOperations) == 0 {
				fmt.Println("No trusted operations configured.")
				return nil
			}
			w := newTabWriter()
			if v.AgentStrict {
				fmt.Fprintln(w, "OPERATION")
			} else {
				fmt.Fprintln(w, "OPERATION\tLABEL")
			}
			for _, id := range v.TrustedOperationIDs() {
				op := v.TrustedOperations[id]
				if v.AgentStrict {
					fmt.Fprintln(w, id)
				} else {
					fmt.Fprintf(w, "%s\t%s\n", id, op.Label)
				}
			}
			return w.Flush()
		},
	}
}

func operationRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run <operation-id>",
		Short: "Run a fixed operation and return only its bounded result",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTrustedOperation(cmd, args[0])
		},
	}
}

func runTrustedOperation(cmd *cobra.Command, id string) error {
	v, pw, err := openOperationVault(cmd.Context(), "operation run")
	if err != nil {
		return err
	}
	clearBytes(pw)
	defer v.Close()

	op, err := v.TrustedOperation(id)
	if err != nil {
		recordAudit("operation-run", id, 0, err)
		return err
	}
	_, env, err := v.ResolveEnv(op.Spec)
	if err != nil {
		err = fmt.Errorf("trusted operation credential is unavailable")
		recordAudit("operation-run", id, 0, err)
		return err
	}
	result, runErr := runtimeop.Run(cmd.Context(), op, env)
	recordAudit("operation-run", id, 1, runErr)
	if runErr != nil {
		if result.Status != 0 {
			return fmt.Errorf("trusted operation failed (HTTP %d)", result.Status)
		}
		return fmt.Errorf("trusted operation failed")
	}
	fmt.Printf("Operation %s succeeded (HTTP %d, %d ms).\n", id, result.Status, result.Duration.Milliseconds())
	return nil
}

func newOperationID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate operation ID: %w", err)
	}
	return "op_" + hex.EncodeToString(raw[:]), nil
}

func parseExpectedStatus(value string) (int, int, error) {
	parts := strings.Split(value, "-")
	if len(parts) > 2 {
		return 0, 0, fmt.Errorf("--expect must be a status code or range such as 200-299")
	}
	lo, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("--expect must be a status code or range such as 200-299")
	}
	hi := lo
	if len(parts) == 2 {
		hi, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, fmt.Errorf("--expect must be a status code or range such as 200-299")
		}
	}
	if lo < 100 || hi > 599 || lo > hi {
		return 0, 0, fmt.Errorf("--expect must stay within 100..599")
	}
	return lo, hi, nil
}
