package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/yemon/calypso/internal/project"
	"github.com/yemon/calypso/internal/vault"
	"golang.org/x/term"
)

func strictCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "strict",
		Short: "Enforce broker-only agent access to credentials",
		Long: `Agent strict mode is the production boundary for coding agents.

While ON, arbitrary secret-bearing commands and every command that exposes
credential names or mutates owner configuration are blocked. Metadata-only
commands and fixed trusted operations remain available. Enabling strict mode
also enables hard lockdown.

Turning strict mode OFF requires the master passphrase typed directly into an
interactive terminal; keychain and environment passphrases are ignored.`,
	}
	cmd.AddCommand(strictOnCmd(), strictOffCmd(), strictStatusCmd())
	return cmd
}

func strictOnCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "on",
		Short: "Enable broker-only agent access and hard lockdown",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			v, pw, err := openMetadataVault(ctx, "strict on")
			if err != nil {
				return err
			}
			defer clearBytes(pw)
			if v.AgentStrict && v.Lockdown && !v.LockdownUnattendedOK {
				fmt.Println("Agent strict mode is already on.")
				return nil
			}
			if err := sanitizeRegisteredEnvFiles(v); err != nil {
				return err
			}
			v.AgentStrict = true
			v.Lockdown = true
			v.LockdownUnattendedOK = false
			if err := saveAndWarn(ctx, v, pw); err != nil {
				return err
			}
			recordAudit("strict-on", "", 0, nil)
			fmt.Println("Agent strict mode is on. Arbitrary secret access is blocked; use trusted operations or the broker.")
			return nil
		},
	}
}

func sanitizeRegisteredEnvFiles(v *vault.Vault) error {
	var paths []string
	for _, projectName := range v.Names() {
		p := v.Projects[projectName]
		for _, envName := range p.EnvNames() {
			env := p.Envs[envName]
			diskVars, err := project.ReadEnvFile(env.Path)
			if err != nil {
				if os.IsNotExist(err) {
					continue
				}
				return fmt.Errorf("cannot enable strict mode: a registered environment file is unreadable")
			}
			if hasUnsyncedEdits(env.Vars, diskVars) {
				return fmt.Errorf("cannot enable strict mode: a registered environment file has unsynced local edits; push or mask it first")
			}
			paths = append(paths, env.Path)
		}
	}
	for _, path := range paths {
		if err := project.WriteStrictEnvFile(path); err != nil {
			return fmt.Errorf("cannot enable strict mode: failed to sanitize a registered environment file")
		}
	}
	return nil
}

func strictOffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "off",
		Short: "Disable agent strict mode (interactive passphrase required)",
		RunE: func(cmd *cobra.Command, args []string) error {
			fd := int(os.Stdin.Fd()) //nolint:gosec // fd fits in int on supported platforms
			if !term.IsTerminal(fd) {
				return fmt.Errorf("strict off requires an interactive terminal (the keychain and %s are ignored on purpose)", canonicalPassphraseEnv)
			}
			fmt.Fprint(os.Stderr, "Master passphrase: ")
			pw, err := term.ReadPassword(fd)
			fmt.Fprintln(os.Stderr)
			if err != nil {
				return fmt.Errorf("reading passphrase: %w", err)
			}
			defer clearBytes(pw)

			ctx := cmd.Context()
			v, err := vault.Load(ctx, vaultPath, pw)
			if err != nil {
				return err
			}
			if !v.AgentStrict {
				fmt.Println("Agent strict mode is already off.")
				return nil
			}
			v.AgentStrict = false
			if err := saveAndWarn(ctx, v, pw); err != nil {
				return err
			}
			recordAudit("strict-off", "", 0, nil)
			fmt.Println("Agent strict mode is off. Hard lockdown remains unchanged.")
			return nil
		},
	}
}

func strictStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show agent strict mode status",
		RunE: func(cmd *cobra.Command, args []string) error {
			v, pw, err := openMetadataVault(cmd.Context(), "strict status")
			if err != nil {
				return err
			}
			clearBytes(pw)
			if v.AgentStrict {
				fmt.Printf("Agent strict mode: on (%d trusted operation(s))\n", len(v.TrustedOperations))
			} else {
				fmt.Println("Agent strict mode: off")
			}
			return nil
		},
	}
}
