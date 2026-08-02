package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/yemon/calypso/internal/vault"
)

// vaultVerifyCmd runs read-only sanity checks on the loaded vault and
// optionally applies trivial repairs with --fix. Defaults to dry-run.
// Exit code is non-zero when any error-severity finding remains, which
// makes it suitable for `calypso vault verify || alert` in cron-style
// monitoring.
func vaultVerifyCmd() *cobra.Command {
	var fix bool
	cmd := &cobra.Command{
		Use:   "verify",
		Short: "Sanity-check the vault contents; report any issues",
		Long: `verify decrypts the vault, walks every project and environment,
and reports anything that looks wrong: missing fields, relative paths,
duplicate keys, name/key mismatches, etc.

By default it's read-only — exit code reflects whether any errors were
found. Pass --fix to apply trivial repairs (timestamps, name sync,
duplicate-key removal) and save the result. Auto-backup preserves the
pre-fix vault. Relative paths are reported but not auto-fixed: calypso
can't know what they were meant to be relative to, so re-add with an
absolute --path.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			v, pw, err := openOwnerVault(ctx, "vault verify")
			if err != nil {
				return err
			}
			if !fix {
				clearBytes(pw)
			} else {
				defer clearBytes(pw)
			}

			pre := vault.Verify(v)
			printFindings("Findings:", pre)

			if !fix {
				return exitOnErrors(pre)
			}

			fixes := vault.Repair(v)
			if len(fixes) == 0 {
				fmt.Println("\nNo fixable issues.")
				return exitOnErrors(pre)
			}

			fmt.Println("\nApplied:")
			for _, f := range fixes {
				fmt.Println("  - " + f)
			}

			if err := saveAndWarn(ctx, v, pw); err != nil {
				return fmt.Errorf("save after repair: %w", err)
			}

			post := vault.Verify(v)
			if len(post) > 0 {
				fmt.Println()
				printFindings("Remaining:", post)
			} else {
				fmt.Println("\nNo issues remain.")
			}
			return exitOnErrors(post)
		},
	}
	cmd.Flags().BoolVar(&fix, "fix", false, "apply trivial repairs and save the vault")
	return cmd
}

func printFindings(header string, fs []vault.Finding) {
	if len(fs) == 0 {
		fmt.Println(header, "none.")
		return
	}
	fmt.Println(header)
	for _, f := range fs {
		fmt.Println("  " + f.String())
	}
}

// exitOnErrors returns a non-nil error (causing a non-zero exit) when any
// finding has severity "error". Warnings alone don't fail the command.
func exitOnErrors(fs []vault.Finding) error {
	for _, f := range fs {
		if f.Severity == "error" {
			return fmt.Errorf("vault has %d unresolved error(s)", countErrors(fs))
		}
	}
	return nil
}

func countErrors(fs []vault.Finding) int {
	n := 0
	for _, f := range fs {
		if f.Severity == "error" {
			n++
		}
	}
	return n
}

// stderrf is a tiny helper so command files don't all need to import "os".
// Kept here next to verify-specific code since that's the only caller today.
func stderrf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, format, args...)
}

var _ = stderrf // reserved for future verbose output; silence unused-fn lint
