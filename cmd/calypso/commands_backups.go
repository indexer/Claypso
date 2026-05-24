package main

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/yemon/calypso/internal/vault"
)

// vaultBackupsCmd groups the auto-backup management subcommands. The
// backups themselves are written automatically on every successful Save;
// these commands let the user inspect and roll back to them.
func vaultBackupsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backups",
		Short: "List and restore auto-backups (one per save, retained by CALYPSO_BACKUP_RETAIN)",
	}
	cmd.AddCommand(vaultBackupsListCmd(), vaultBackupsRestoreCmd())
	return cmd
}

func vaultBackupsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show all auto-backups for the current vault, oldest first",
		RunE: func(cmd *cobra.Command, args []string) error {
			names, err := vault.BackupNames(vaultPath)
			if err != nil {
				return err
			}
			if len(names) == 0 {
				fmt.Println("No backups yet. They appear after the first successful save.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tAGE")
			now := time.Now().UTC()
			for _, n := range names {
				// Filename stem is the backup timestamp.
				stem := n[:len(n)-len(".enc")]
				if t, err := time.Parse("20060102T150405.000000000Z", stem); err == nil {
					fmt.Fprintf(w, "%s\t%s ago\n", n, formatAge(now.Sub(t)))
				} else {
					fmt.Fprintf(w, "%s\t?\n", n)
				}
			}
			return w.Flush()
		},
	}
}

func vaultBackupsRestoreCmd() *cobra.Command {
	var force bool
	c := &cobra.Command{
		Use:   "restore <backup-name>",
		Short: "Replace the current vault with the named backup",
		Long: `Restore copies the named backup over the current vault file.

The current vault is first saved to <vault>.pre-restore.bak so the
restore can be undone by swapping it back. The backup file itself is
left in place.

Use 'vault backups list' to see available names.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !force {
				ok, err := confirmYesNo(os.Stderr,
					fmt.Sprintf("Restore %q over %s (current vault → %s.pre-restore.bak)?", args[0], vaultPath, vaultPath),
					false)
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("restore cancelled")
				}
			}
			if err := vault.RestoreBackup(cmd.Context(), vaultPath, args[0]); err != nil {
				return err
			}
			fmt.Printf("Restored %s from backup %s\n", vaultPath, args[0])
			fmt.Printf("Previous vault saved to %s.pre-restore.bak\n", vaultPath)
			return nil
		},
	}
	c.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation prompt")
	return c
}

// formatAge renders a duration as a human-friendly short string.
func formatAge(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}
