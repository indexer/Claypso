package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/yemon/calypso/internal/vault"
	"golang.org/x/term"
)

// lockdownCmd toggles the vault's Lockdown flag. While it is on, every
// reveal-class command (get --reveal, diff --reveal, drift --reveal, plain
// pull, vault export-project) refuses to run — regardless of TTY or
// CALYPSO_UNATTENDED — so a coding agent that can unlock the vault still
// cannot print or write real values. Masked and inject/wipe workflows keep
// working.
func lockdownCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "lockdown",
		Short: "Block reveal-class commands until unlocked interactively",
		Long: `lockdown stores an agent-safety flag inside the encrypted vault.

While ON, commands that reveal real values are refused entirely:
  get --reveal, diff --reveal, drift --reveal, pull (real values),
  vault export-project

Masked commands keep working: list, get, pull --safe, pull -- cmd
(with output scrubbing), drift, gaps.

Turning lockdown OFF requires typing the master passphrase at an
interactive terminal — the keychain and CALYPSO_PASSPHRASE are
deliberately ignored, so an unattended agent cannot flip it back.`,
	}
	cmd.AddCommand(lockdownOnCmd(), lockdownOffCmd(), lockdownStatusCmd())
	return cmd
}

func lockdownOnCmd() *cobra.Command {
	var allowUnattended bool
	cmd := &cobra.Command{
		Use:   "on",
		Short: "Enable lockdown (reveal-class commands refuse to run)",
		Long: `lockdown on blocks reveal-class commands until lifted interactively.

With --allow-unattended, contexts that explicitly set CALYPSO_UNATTENDED=1
(deploy pipelines, CI) stay exempt. Use this on vault copies that feed a
deployment; note it weakens lockdown to reveal-gate strength for any
process willing to set that variable, so keep the hard default on the
vault your coding agent can reach.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			v, pw, err := openMetadataVault(ctx, "lockdown on")
			if err != nil {
				return err
			}
			defer clearBytes(pw)
			if v.AgentStrict && allowUnattended {
				return fmt.Errorf("lockdown --allow-unattended is blocked while agent strict mode is on")
			}
			if v.Lockdown && v.LockdownUnattendedOK == allowUnattended {
				fmt.Println("Lockdown is already on in this mode.")
				return nil
			}
			v.Lockdown = true
			v.LockdownUnattendedOK = allowUnattended
			if err := saveAndWarn(ctx, v, pw); err != nil {
				return err
			}
			mode := "lockdown-on"
			if allowUnattended {
				mode = "lockdown-on-allow-unattended"
			}
			recordAudit(mode, "", 0, nil)
			if allowUnattended {
				fmt.Println("Lockdown on (unattended pipelines with CALYPSO_UNATTENDED=1 stay exempt).")
			} else {
				fmt.Println("Lockdown on. Reveal-class commands are blocked; run `calypso lockdown off` in your own terminal to lift it.")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&allowUnattended, "allow-unattended", false, "keep reveal-class commands working where CALYPSO_UNATTENDED=1 is set (deploy pipelines)")
	return cmd
}

func lockdownOffCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "off",
		Short: "Disable lockdown (requires typing the passphrase at a terminal)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !term.IsTerminal(int(os.Stdin.Fd())) { //nolint:gosec // fd fits in int on supported platforms
				return fmt.Errorf("lockdown off requires an interactive terminal (the keychain and %s are ignored on purpose)", canonicalPassphraseEnv)
			}
			// Prove a human is present: read the passphrase directly from the
			// terminal, bypassing the keychain and passphrase env vars that an
			// unattended process could supply.
			fmt.Fprint(os.Stderr, "Master passphrase: ")
			pw, err := term.ReadPassword(int(os.Stdin.Fd())) //nolint:gosec // fd fits in int on supported platforms
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
			if v.AgentStrict {
				return fmt.Errorf("lockdown off is blocked while agent strict mode is on; run `calypso strict off` first")
			}
			if !v.Lockdown && !v.LockdownUnattendedOK {
				fmt.Println("Lockdown is already off.")
				return nil
			}
			v.Lockdown = false
			v.LockdownUnattendedOK = false
			if err := saveAndWarn(ctx, v, pw); err != nil {
				return err
			}
			recordAudit("lockdown-off", "", 0, nil)
			fmt.Println("Lockdown off. Reveal-class commands are available again.")
			return nil
		},
	}
}

func lockdownStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether lockdown is on",
		RunE: func(cmd *cobra.Command, args []string) error {
			v, pw, err := openMetadataVault(cmd.Context(), "lockdown status")
			if err != nil {
				return err
			}
			clearBytes(pw)
			switch {
			case v.Lockdown && v.LockdownUnattendedOK:
				fmt.Println("Lockdown: on, unattended pipelines exempt (CALYPSO_UNATTENDED=1)")
			case v.Lockdown:
				fmt.Println("Lockdown: on (reveal-class commands are blocked)")
			default:
				fmt.Println("Lockdown: off")
			}
			return nil
		},
	}
}
