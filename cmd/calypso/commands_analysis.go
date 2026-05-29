package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/yemon/calypso/internal/analysis"
	"github.com/yemon/calypso/internal/dashboard"
)

// newTabWriter is the one place that knows the column layout calypso uses.
// Centralising it keeps `list`, `diff`, and `gaps` aligned with each other.
func newTabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
}

func diffCmd() *cobra.Command {
	var reveal bool
	cmd := &cobra.Command{
		Use:   "diff <A[@env]> <B[@env]>",
		Short: "Compare two environments (same or different projects)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, pw, err := openVault(cmd.Context())
			if err != nil {
				return err
			}
			clearBytes(pw)
			entries, err := analysis.Diff(v, args[0], args[1])
			if err != nil {
				return err
			}
			w := newTabWriter()
			fmt.Fprintf(w, "KEY\t%s\t%s\tSTATUS\n", args[0], args[1])
			for _, e := range entries {
				status := "differs"
				switch {
				case e.Equal:
					status = "same"
				case e.InA && !e.InB:
					status = "only in " + args[0]
				case !e.InA && e.InB:
					status = "only in " + args[1]
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					e.Key, cell(e.ValueA, e.InA, reveal), cell(e.ValueB, e.InB, reveal), status)
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&reveal, "reveal", false, "show actual values instead of masking")
	return cmd
}

// gapsCmd shows both cross-project gaps (key missing in one project's env
// when other projects' same-named env have it) and intra-project gaps
// (key in one env of a project but missing from its sibling envs).
func gapsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "gaps",
		Short: "Find keys that some envs have but others are missing",
		RunE: func(cmd *cobra.Command, args []string) error {
			v, pw, err := openVault(cmd.Context())
			if err != nil {
				return err
			}
			clearBytes(pw)
			cross := analysis.FindCrossProjectGaps(v)
			intra := analysis.FindIntraProjectGaps(v)
			if len(cross) == 0 && len(intra) == 0 {
				fmt.Println("No gaps — every shared key is present everywhere.")
				return nil
			}
			if len(cross) > 0 {
				fmt.Println("Cross-project gaps (same env name across projects):")
				if err := printGaps(cross); err != nil {
					return err
				}
				fmt.Println()
			}
			if len(intra) > 0 {
				fmt.Println("Intra-project gaps (across envs of the same project):")
				if err := printGaps(intra); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

func printGaps(gaps []analysis.Gap) error {
	w := newTabWriter()
	fmt.Fprintln(w, "MISSING IN\tKEY\tDEFINED IN")
	for _, g := range gaps {
		fmt.Fprintf(w, "%s\t%s\t%s\n", g.Ref, g.Key, joinRefs(g.DefinedIn))
	}
	return w.Flush()
}

func joinRefs(refs []analysis.EnvRef) string {
	out := ""
	for i, r := range refs {
		if i > 0 {
			out += ", "
		}
		out += r.String()
	}
	return out
}

func dashboardCmd() *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:   "dashboard",
		Short: "Open a localhost web view of all environments",
		RunE: func(cmd *cobra.Command, args []string) error {
			v, pw, err := openVault(cmd.Context())
			if err != nil {
				return err
			}
			clearBytes(pw)
			v.Close() // read-only server: drop the derived key it will never use again
			return dashboard.Serve(v, port)
		},
	}
	cmd.Flags().IntVar(&port, "port", 7777, "localhost port for the dashboard")
	return cmd
}

func cell(val string, present, reveal bool) string {
	if !present {
		return "—"
	}
	// Comparison views keep the edge-character hint so differing values are
	// visually distinguishable; the no-leak fixed-width mask is reserved for
	// the shareable `get` output.
	return maybeMask(val, reveal, true)
}
