package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/yemon/calypso/internal/analysis"
	"github.com/yemon/calypso/internal/vault"
)

// driftCmd compares each env's vault contents against the on-disk .env
// file at its path. Useful for spotting manual edits made outside calypso
// (someone tweaked .env locally, never ran `push`) and as a CI guard.
func driftCmd() *cobra.Command {
	var details, reveal bool
	cmd := &cobra.Command{
		Use:   "drift [project[@env]]",
		Short: "Compare envs' vault contents against their on-disk .env files",
		Long: `drift parses each env's .env file from disk and compares it to what
the vault holds. With no argument it checks every env; with a
'project[@env]' argument it checks just that one.

Exit code is non-zero if any drift is detected, so this is suitable for
CI guards ("alert me if .env was edited outside calypso").

  --details  list per-key changes, not just counts
  --reveal   show real values in --details output (default: masked)`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, pw, err := openVault(cmd.Context())
			if err != nil {
				return err
			}
			clearBytes(pw)

			refs := pickRefs(v, args)
			if len(refs) == 0 {
				fmt.Println("No envs to check.")
				return nil
			}

			anyDrift := false
			for _, r := range refs {
				p, e, err := v.ResolveEnv(r)
				if err != nil {
					return err
				}
				var rep analysis.DriftReport
				if details {
					rep, err = analysis.DriftDetails(e, p.Name)
				} else {
					rep, err = analysis.Drift(e, p.Name)
				}
				if err != nil {
					return fmt.Errorf("%s: %w", r, err)
				}
				if err := printDrift(rep, details, reveal); err != nil {
					return err
				}
				if rep.HasDrift() {
					anyDrift = true
				}
			}
			if anyDrift {
				return fmt.Errorf("drift detected")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&details, "details", false, "list per-key differences")
	cmd.Flags().BoolVar(&reveal, "reveal", false, "show real values in --details (default masked)")
	return cmd
}

// pickRefs returns the list of name@env specs to check. If args is empty,
// every env of every project is checked. If args[0] is provided, it's
// passed through verbatim — ResolveEnv handles `name` (implicit single env)
// and `name@env`.
func pickRefs(v *vault.Vault, args []string) []string {
	if len(args) == 1 {
		return []string{args[0]}
	}
	var out []string
	for _, name := range v.Names() {
		p := v.Projects[name]
		for _, en := range p.EnvNames() {
			out = append(out, name+"@"+en)
		}
	}
	return out
}

func printDrift(r analysis.DriftReport, details, reveal bool) error {
	fmt.Printf("%s  (%s)\n", r.Ref, r.Path)
	if r.Missing {
		fmt.Println("  .env file missing on disk — every vault key counts as removed.")
	} else {
		c := r.Counts
		fmt.Printf("  +%d added  ~%d changed  -%d removed  =%d unchanged\n",
			c.Added, c.Changed, c.Removed, c.Unchanged)
	}
	if !details {
		fmt.Println()
		return nil
	}
	w := newTabWriter()
	fmt.Fprintln(w, "  KEY\tVAULT\tDISK\tKIND")
	for _, e := range r.Entries {
		if e.Kind == analysis.DriftSame {
			continue
		}
		fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n",
			e.Key,
			maybeMask(e.VaultVal, reveal, true),
			maybeMask(e.DiskVal, reveal, true),
			e.Kind)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout)
	return nil
}
