package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yemon/calypso/internal/analysis"
	"github.com/yemon/calypso/internal/project"
)

// exposureCmd reports .env files that currently hold real vault values on
// disk — the state a forgotten plain `pull` leaves behind, and the easiest
// way for an AI agent (or anything else) to read secrets without touching
// calypso at all.
func exposureCmd() *cobra.Command {
	var maxAge time.Duration
	var fix, force bool
	cmd := &cobra.Command{
		Use:   "exposure [project[@env]]",
		Short: "Warn when a .env holds real values on disk (and for how long)",
		Long: `exposure scans each env's on-disk .env and reports keys whose value
matches the vault's real value ("hot"). Masked (****), empty, and
rotated values never count, and neither do values shorter than 4 bytes.

Age is measured from the file's modification time. The exit code is
non-zero when any env has been hot for at least --max-age, so this
works as a cron or CI guard:

  calypso exposure --max-age 30m || notify "calypso: .env exposed"

Use --max-age 0 to fail on any hot env regardless of age.

  --fix   rewrite hot files with masked (****) values. Refused when the
          file also holds local edits not yet pushed to the vault,
          unless --force is set.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, pw, err := openOwnerVault(cmd.Context(), "exposure")
			if err != nil {
				return err
			}
			clearBytes(pw)

			refs := pickRefs(v, args)
			if len(refs) == 0 {
				fmt.Println("No envs to check.")
				return nil
			}

			now := time.Now()
			hot, overdue := 0, 0
			for _, r := range refs {
				p, e, err := v.ResolveEnv(r)
				if err != nil {
					return err
				}
				rep, err := analysis.Exposure(e, p.Name, now)
				if err != nil {
					return fmt.Errorf("%s: %w", r, err)
				}
				if !rep.Hot() {
					continue
				}
				hot++
				fmt.Printf("%s  (%s)\n", rep.Ref, rep.Path)
				fmt.Printf("  %d real value(s) on disk for %s: %s\n",
					len(rep.HotKeys), rep.Age.Round(time.Second), strings.Join(rep.HotKeys, ", "))

				if fix {
					if fixExposed(rep, e, force) {
						continue // remediated — doesn't count as overdue
					}
				} else {
					fmt.Printf("  fix: calypso pull %s --safe\n", rep.Ref)
				}
				if rep.Age >= maxAge {
					overdue++
				}
			}

			if hot == 0 {
				fmt.Println("No exposed .env files.")
				return nil
			}
			if overdue > 0 {
				return fmt.Errorf("%d env(s) exposed for at least %s", overdue, maxAge)
			}
			return nil
		},
	}
	cmd.Flags().DurationVar(&maxAge, "max-age", 30*time.Minute, "fail (non-zero exit) when hot at least this long; 0 fails on any hot env")
	cmd.Flags().BoolVar(&fix, "fix", false, "rewrite hot .env files with masked (****) values")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "with --fix, overwrite even when the file has local edits not in the vault")
	return cmd
}

// fixExposed rewrites a hot .env with masked values. Returns true when the
// file was rewritten. Files that also carry unsynced local edits are left
// alone unless force is set — --fix must never silently destroy work the
// vault doesn't have yet.
func fixExposed(rep analysis.ExposureReport, e *project.Environment, force bool) bool {
	diskVars, err := project.ReadEnvFile(e.Path)
	if err == nil && hasUnsyncedEdits(e.Vars, diskVars) && !force {
		fmt.Printf("  skipped --fix: file has local edits not in the vault; run `calypso push %s` first or use --force\n", rep.Ref)
		return false
	}
	if err := project.WriteSafeEnvFile(e.Path, e.Vars); err != nil {
		fmt.Fprintf(os.Stderr, "  --fix failed for %s: %v\n", rep.Path, err)
		return false
	}
	fmt.Printf("  fixed: rewrote %s with masked values\n", rep.Path)
	return true
}
