package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yemon/calypso/internal/project"
)

// syncCmd pushes an env's real values into a platform's secret store by
// shelling out to that platform's own CLI. Values travel via stdin (never
// argv, never a temp file), the child's output runs through the scrubber so
// a CLI that echoes a value prints <concealed:KEY>, and the passphrase env
// vars are stripped from the child.
func syncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Push an env's values into a platform secret store (fly, vercel, k8s)",
		Long: `sync replaces the shell-pipe dance for deploy targets that hold their
own secret store. It is a reveal-class operation: it needs an
interactive terminal or CALYPSO_UNATTENDED=1, is blocked by lockdown
(the --allow-unattended exemption applies — fitting, since sync usually
runs in CI), and every run is audited.

Values are delivered on stdin to the platform CLI; they never appear in
process arguments or on disk. The CLI's output is scrubbed.`,
	}
	cmd.AddCommand(syncFlyCmd(), syncVercelCmd(), syncK8sCmd())
	return cmd
}

// syncTarget opens the vault, resolves the env, applies the reveal gate,
// and records the audit event for a sync subcommand.
func syncTarget(cmd *cobra.Command, spec, platform string) ([]project.Var, error) {
	v, pw, err := openOwnerVault(cmd.Context(), "sync "+platform)
	if err != nil {
		return nil, err
	}
	clearBytes(pw)
	_, e, err := v.ResolveEnv(spec)
	if err != nil {
		return nil, err
	}
	gateErr := guardReveal(v.Lockdown, v.LockdownUnattendedOK, "sync "+platform)
	recordAudit("sync-"+platform, spec, len(e.Vars), gateErr)
	if gateErr != nil {
		return nil, gateErr
	}
	if len(e.Vars) == 0 {
		return nil, fmt.Errorf("%s has no variables to sync", spec)
	}
	return e.Vars, nil
}

// runPlatformCLI executes the platform CLI with payload on stdin, scrubbed
// stdout/stderr, and the passphrase env vars stripped.
func runPlatformCLI(vars []project.Var, payload string, name string, args ...string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("%s not found in PATH — install it or use --dry-run to see the payload shape", name)
	}
	c := exec.Command(name, args...)
	c.Stdin = strings.NewReader(payload)
	c.Env = stripSensitiveEnv(os.Environ())
	outScrub := newScrubWriter(os.Stdout, vars)
	errScrub := newScrubWriter(os.Stderr, vars)
	c.Stdout = outScrub
	c.Stderr = errScrub
	runErr := c.Run()
	if err := outScrub.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "calypso: flushing scrubbed stdout: %v\n", err)
	}
	if err := errScrub.Close(); err != nil {
		fmt.Fprintf(os.Stderr, "calypso: flushing scrubbed stderr: %v\n", err)
	}
	if runErr != nil {
		return fmt.Errorf("%s failed: %w", name, runErr)
	}
	return nil
}

func printDryRun(vars []project.Var, command string) {
	fmt.Printf("Dry run — would send %d key(s) via stdin to: %s\n", len(vars), command)
	for _, kv := range vars {
		fmt.Printf("  %s=%s\n", kv.Key, maskFixed)
	}
}

// --- fly ---------------------------------------------------------------

// buildFlyPayload renders KEY=VALUE lines for `flyctl secrets import`.
// The line format cannot carry newlines inside a value, so those keys are
// rejected outright rather than silently corrupted.
func buildFlyPayload(vars []project.Var) (string, error) {
	var b strings.Builder
	var bad []string
	for _, kv := range vars {
		val := kv.Value.Reveal()
		if strings.ContainsAny(val, "\n\r") {
			bad = append(bad, kv.Key)
			continue
		}
		b.WriteString(kv.Key)
		b.WriteByte('=')
		b.WriteString(val)
		b.WriteByte('\n')
	}
	if len(bad) > 0 {
		return "", fmt.Errorf("fly secrets import cannot carry multiline values; offending key(s): %s", strings.Join(bad, ", "))
	}
	return b.String(), nil
}

func syncFlyCmd() *cobra.Command {
	var app string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "fly <project[@env]>",
		Short: "Push values to Fly.io via `flyctl secrets import`",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vars, err := syncTarget(cmd, args[0], "fly")
			if err != nil {
				return err
			}
			payload, err := buildFlyPayload(vars)
			if err != nil {
				return err
			}
			cliArgs := []string{"secrets", "import"}
			if app != "" {
				cliArgs = append(cliArgs, "-a", app)
			}
			if dryRun {
				printDryRun(vars, "flyctl "+strings.Join(cliArgs, " "))
				return nil
			}
			if err := runPlatformCLI(vars, payload, "flyctl", cliArgs...); err != nil {
				return err
			}
			fmt.Printf("Synced %d key(s) from %s to Fly.\n", len(vars), args[0])
			return nil
		},
	}
	cmd.Flags().StringVarP(&app, "app", "a", "", "Fly app name (defaults to the fly.toml in the working directory)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the masked payload and command without sending")
	return cmd
}

// --- vercel ------------------------------------------------------------

func syncVercelCmd() *cobra.Command {
	var target string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "vercel <project[@env]>",
		Short: "Push values to Vercel via `vercel env add` (one call per key)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vars, err := syncTarget(cmd, args[0], "vercel")
			if err != nil {
				return err
			}
			if dryRun {
				printDryRun(vars, fmt.Sprintf("vercel env add <KEY> %s --force", target))
				return nil
			}
			for _, kv := range vars {
				// --force upserts: without it `env add` fails on existing keys.
				if err := runPlatformCLI(vars, kv.Value.Reveal(),
					"vercel", "env", "add", kv.Key, target, "--force"); err != nil {
					return fmt.Errorf("key %s: %w", kv.Key, err)
				}
			}
			fmt.Printf("Synced %d key(s) from %s to Vercel (%s).\n", len(vars), args[0], target)
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "target", "production", "Vercel environment: production, preview, or development")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the masked payload and command without sending")
	return cmd
}

// --- k8s ---------------------------------------------------------------

// k8sSecretName sanitizes a calypso project name into an RFC 1123 subdomain
// label: lowercase alphanumerics and '-'.
func k8sSecretName(name string) string {
	lower := strings.ToLower(name)
	var b strings.Builder
	for _, r := range lower {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		case r == '_' || r == '.' || r == '@':
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// buildK8sSecretYAML renders a v1 Secret manifest with base64 data, ready
// for `kubectl apply -f -`. base64 carries any byte sequence, so multiline
// values are fine here.
func buildK8sSecretYAML(name, namespace string, vars []project.Var) string {
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Secret\nmetadata:\n  name: ")
	b.WriteString(name)
	b.WriteByte('\n')
	if namespace != "" {
		b.WriteString("  namespace: ")
		b.WriteString(namespace)
		b.WriteByte('\n')
	}
	b.WriteString("type: Opaque\ndata:\n")
	for _, kv := range vars {
		b.WriteString("  ")
		b.WriteString(kv.Key)
		b.WriteString(": ")
		b.WriteString(base64.StdEncoding.EncodeToString([]byte(kv.Value.Reveal())))
		b.WriteByte('\n')
	}
	return b.String()
}

func syncK8sCmd() *cobra.Command {
	var name, namespace string
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "k8s <project[@env]>",
		Short: "Push values to Kubernetes as a Secret via `kubectl apply`",
		Long: `Builds a v1 Secret manifest in memory and pipes it to
'kubectl apply -f -'. apply upserts, so re-running updates the Secret in
place. Values are base64-encoded in the manifest and never touch disk.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			vars, err := syncTarget(cmd, args[0], "k8s")
			if err != nil {
				return err
			}
			secretName := name
			if secretName == "" {
				secretName = k8sSecretName(strings.SplitN(args[0], "@", 2)[0])
			}
			if secretName == "" {
				return fmt.Errorf("cannot derive a valid Secret name from %q; pass --name", args[0])
			}
			if dryRun {
				printDryRun(vars, fmt.Sprintf("kubectl apply -f -   (Secret %q, namespace %q)", secretName, namespace))
				return nil
			}
			manifest := buildK8sSecretYAML(secretName, namespace, vars)
			if err := runPlatformCLI(vars, manifest, "kubectl", "apply", "-f", "-"); err != nil {
				return err
			}
			fmt.Printf("Synced %d key(s) from %s to Kubernetes Secret %q.\n", len(vars), args[0], secretName)
			return nil
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "Secret name (default: sanitized project name)")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace (default: current context's)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the masked payload and command without sending")
	return cmd
}
