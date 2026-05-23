package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yemon/calypso/internal/project"
)

// vaultCmd groups maintenance commands that operate on the vault file
// itself rather than its contents. Today it only hosts `downgrade`; future
// commands like `verify` or `rekey` belong here too.
func vaultCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "vault",
		Short: "Maintenance commands for the vault file itself",
	}
	cmd.AddCommand(vaultDowngradeCmd())
	return cmd
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

The current vault is copied to <vault>.v2.bak before being overwritten,
so you can restore it by replacing the vault file with the backup.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			v, pw, err := openVault(ctx)
			if err != nil {
				return err
			}
			defer clearBytes(pw)

			if blockers := v.V1Blockers(); len(blockers) > 0 {
				return fmt.Errorf("cannot downgrade — these projects need v2:\n  - %s\nremove the extra envs or rename to %q before retrying",
					strings.Join(blockers, "\n  - "), project.DefaultEnvName)
			}

			bak := vaultPath + ".v2.bak"
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
