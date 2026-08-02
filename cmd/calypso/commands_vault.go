package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yemon/calypso/internal/keychain"
	"github.com/yemon/calypso/internal/project"
	"golang.org/x/term"
)

// vaultCmd groups maintenance commands that operate on the vault file
// itself rather than its contents.
func vaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault",
		Short: "Maintenance commands for the vault file itself",
	}
	cmd.AddCommand(vaultDowngradeCmd(), vaultBackupsCmd(), vaultVerifyCmd(), vaultExportProjectCmd(), vaultRekeyCmd())
	return cmd
}

// newPassphraseEnv supplies the NEW passphrase for an unattended
// `vault rekey` (CI rotation). It is deliberately distinct from
// CALYPSO_PASSPHRASE, which always means the current passphrase.
const newPassphraseEnv = "CALYPSO_NEW_PASSPHRASE" //nolint:gosec // env var name, not a credential

func vaultRekeyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rekey",
		Short: "Change the master passphrase (re-encrypts the vault)",
		Long: `rekey re-encrypts the vault under a new master passphrase.

The current passphrase unlocks the vault as usual (keychain,
CALYPSO_PASSPHRASE, or prompt). The new passphrase is read from an
interactive prompt — or from CALYPSO_NEW_PASSPHRASE for unattended
rotation in CI (which also needs CALYPSO_UNATTENDED=1).

Afterwards:
  - the old-passphrase blob is kept at <vault>.pre-rekey.bak
  - existing auto-backups and exports still need the OLD passphrase
  - a stored keychain entry is updated to the new passphrase
  - update CALYPSO_PASSPHRASE wherever CI/workspaces set it

rekey is refused while lockdown is on: a process that merely knows the
old passphrase must not be able to rotate the vault away from its owner.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			v, pw, err := openOwnerVault(ctx, "vault rekey")
			if err != nil {
				return err
			}
			defer clearBytes(pw)
			// Control-class gate: hard-blocked under lockdown (no CI
			// exemption), headless only with CALYPSO_UNATTENDED=1.
			gateErr := guardReveal(v.Lockdown, false, "vault rekey")
			recordAudit("vault-rekey", "", 0, gateErr)
			if gateErr != nil {
				return gateErr
			}

			newPw, fromEnv, err := readRekeyPassphrase()
			if err != nil {
				return err
			}
			defer clearBytes(newPw)
			if bytes.Equal(pw, newPw) {
				return fmt.Errorf("new passphrase is identical to the current one")
			}

			if err := v.Rekey(ctx, vaultPath, newPw); err != nil {
				return err
			}
			warnBackupErr(v)

			fmt.Println("Vault re-encrypted with the new passphrase.")
			fmt.Printf("Old-passphrase copy kept at %s.pre-rekey.bak — delete it once you've verified access.\n", vaultPath)
			fmt.Println("Note: existing auto-backups and exports still decrypt with the OLD passphrase.")

			refreshKeychainAfterRekey(newPw)
			if fromEnv {
				fmt.Printf("Reminder: update %s wherever it is configured (CI secrets, workspaces).\n", canonicalPassphraseEnv)
			}
			return nil
		},
	}
}

// readRekeyPassphrase returns the new passphrase: from CALYPSO_NEW_PASSPHRASE
// when set (CI rotation), otherwise from a double interactive prompt that
// reads the terminal directly — CALYPSO_PASSPHRASE holds the OLD passphrase
// and must never be mistaken for the new one.
func readRekeyPassphrase() ([]byte, bool, error) {
	if val := os.Getenv(newPassphraseEnv); val != "" {
		if len(val) < minPassphraseLen {
			return nil, true, fmt.Errorf("%s must be at least %d characters", newPassphraseEnv, minPassphraseLen)
		}
		return []byte(val), true, nil
	}
	fd := int(os.Stdin.Fd()) //nolint:gosec // fd fits in int on supported platforms
	if !term.IsTerminal(fd) {
		return nil, false, fmt.Errorf("vault rekey needs an interactive terminal for the new passphrase, or set %s", newPassphraseEnv)
	}
	fmt.Fprint(os.Stderr, "New master passphrase: ")
	a, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		return nil, false, fmt.Errorf("reading passphrase: %w", err)
	}
	if len(a) < minPassphraseLen {
		clearBytes(a)
		return nil, false, fmt.Errorf("passphrase must be at least %d characters", minPassphraseLen)
	}
	fmt.Fprint(os.Stderr, "Confirm new passphrase: ")
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(os.Stderr)
	if err != nil {
		clearBytes(a)
		return nil, false, fmt.Errorf("reading passphrase: %w", err)
	}
	if !bytes.Equal(a, b) {
		clearBytes(a)
		clearBytes(b)
		return nil, false, fmt.Errorf("passphrases do not match")
	}
	clearBytes(b)
	return a, false, nil
}

// refreshKeychainAfterRekey replaces a stored keychain passphrase with the
// new one. Skipped entirely in unattended runs — CI must never touch the
// user's keychain.
func refreshKeychainAfterRekey(newPw []byte) {
	if unattended() || !keychain.Available() {
		return
	}
	stored, err := keychain.Retrieve()
	if err != nil {
		return // nothing stored; nothing to refresh
	}
	clearBytes(stored)
	if err := keychain.Store(newPw); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: keychain update failed (%v); run `calypso keychain save` manually.\n", err)
		return
	}
	fmt.Println("Keychain entry updated to the new passphrase.")
}

func vaultDowngradeCmd() *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "downgrade",
		Short: "Rewrite the vault in the legacy v1 format (single-env projects only)",
		Long: `Downgrade rewrites the vault file in the legacy v1 schema so older
calypso binaries can open it.

It only succeeds when every project has exactly one environment named
"default" — anything richer (multiple envs per project, or a single env
with a custom name) cannot round-trip through v1 and is reported as a
blocker. Remove the extra envs (or rename them by adding a "default" env
and removing the named one) before retrying.

The current vault is copied to <vault>.vN.bak before being overwritten,
so you can restore it by replacing the vault file with the backup.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			v, pw, err := openOwnerVault(ctx, "vault downgrade")
			if err != nil {
				return err
			}
			defer clearBytes(pw)

			if blockers := v.V1Blockers(); len(blockers) > 0 {
				return fmt.Errorf("cannot downgrade — these projects need v2:\n  - %s\nremove the extra envs or rename to %q before retrying",
					strings.Join(blockers, "\n  - "), project.DefaultEnvName)
			}

			bak := fmt.Sprintf("%s.v%d.bak", vaultPath, v.LoadedVersion())
			if !force {
				ok, err := confirmYesNo(os.Stderr,
					fmt.Sprintf("Rewrite %s in v1 format (backup → %s)?", vaultPath, bak), false)
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("downgrade cancelled")
				}
			}

			if err := v.Downgrade(ctx, vaultPath, pw); err != nil {
				return err
			}
			fmt.Printf("Downgraded %s to v1.\n", vaultPath)
			if _, err := os.Stat(bak); err == nil {
				fmt.Printf("Backup: %s\n", bak)
			}
			return nil
		},
	}
	c.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation prompt")
	return c
}
