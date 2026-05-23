package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/yemon/calypso/internal/analysis"
	"github.com/yemon/calypso/internal/dashboard"
	"github.com/yemon/calypso/internal/project"
)

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all registered projects",
		RunE: func(cmd *cobra.Command, args []string) error {
			v, pw, err := openVault(cmd.Context())
			if err != nil {
				return err
			}
			clearBytes(pw)
			if len(v.Projects) == 0 {
				fmt.Println("No projects yet. Use `calypso add <name> --path ./.env`.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "PROJECT\tVARS\tUPDATED\tPATH")
			for _, name := range v.Names() {
				p := v.Projects[name]
				fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", p.Name, len(p.Vars), p.UpdatedAt, p.Path)
			}
			return w.Flush()
		},
	}
}

func pullCmd() *cobra.Command {
	var safe, example bool
	cmd := &cobra.Command{
		Use:   "pull <project> [-- command...]",
		Short: "Write the vault's values out to the project's .env file",
		Long: `pull writes variables from the vault to the project's .env file.

With --safe, values are replaced with placeholders (****) so you can share
the .env with an LLM or teammate without exposing real secrets.

With --example, values are left empty so you can create a .env.example
template for documentation or git.

A trailing command (after --) is executed with the real .env in place,
then the .env is immediately overwritten with safe (****) values when the
command exits — even on failure or interrupt.

Flags:
  -s, --safe      write masked **** values (LLM-safe)
  -e, --example   write empty values (.env.example template)`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectName := args[0]
			wipeCmd := args[1:]

			v, pw, err := openVault(cmd.Context())
			if err != nil {
				return err
			}
			clearBytes(pw)
			p, err := v.Project(projectName)
			if err != nil {
				return err
			}

			switch {
			case example:
				if err := project.WriteExampleEnvFile(p.Path, p.Vars); err != nil {
					return err
				}
				fmt.Printf("Wrote %d key(s) (empty) to %s\n", len(p.Vars), p.Path)
				return nil
			case safe:
				if err := project.WriteSafeEnvFile(p.Path, p.Vars); err != nil {
					return err
				}
				fmt.Printf("Wrote %d safe variable(s) to %s\n", len(p.Vars), p.Path)
				return nil
			case len(wipeCmd) > 0:
				return wipeAndRun(p.Path, p.Vars, wipeCmd)
			default:
				if err := project.WriteEnvFile(p.Path, p.Vars); err != nil {
					return err
				}
				fmt.Printf("Wrote %d variable(s) to %s\n", len(p.Vars), p.Path)
				return nil
			}
		},
	}
	cmd.Flags().BoolVarP(&safe, "safe", "s", false, "write masked values (****) for LLM/teammate sharing")
	cmd.Flags().BoolVarP(&example, "example", "e", false, "write empty values for .env.example")
	return cmd
}

// wipeAndRun writes real .env, executes the command, and overwrites with safe values.
func wipeAndRun(envPath string, vars []project.Var, command []string) error {
	if err := project.WriteEnvFile(envPath, vars); err != nil {
		return err
	}

	c := exec.Command(command[0], command[1:]...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	c.Env = os.Environ()
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	runErr := c.Run()

	if err := project.WriteSafeEnvFile(envPath, vars); err != nil {
		fmt.Fprintf(os.Stderr, "calypso: failed to wipe .env after command: %v\n", err)
	}

	if runErr != nil {
		if exitErr, ok := runErr.(*exec.ExitError); ok {
			return fmt.Errorf("command exited with %v", exitErr)
		}
		return fmt.Errorf("command failed: %w", runErr)
	}
	return nil
}

func pushCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "push <project>",
		Short: "Read the project's .env file back into the vault",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			v, pw, err := openVault(ctx)
			if err != nil {
				return err
			}
			defer clearBytes(pw)
			p, err := v.Project(args[0])
			if err != nil {
				return err
			}
			vars, err := project.ReadEnvFile(p.Path)
			if err != nil {
				return fmt.Errorf("reading %s: %w", p.Path, err)
			}

			if !force && len(p.Vars) > 0 {
				showPushDiff(p, vars)
				ok, err := confirmYesNo(os.Stderr, "Overwrite vault values with .env?", false)
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("push cancelled")
				}
			}

			p.Vars = vars
			v.Touch(args[0])
			if err := v.Save(ctx, vaultPath, pw); err != nil {
				return err
			}
			fmt.Printf("Imported %d variable(s) from %s into %q.\n", len(vars), p.Path, args[0])
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation prompt")
	return cmd
}

func showPushDiff(p *project.Project, incoming []project.Var) {
	c := analysis.DiffCounts(p.Vars, incoming)
	fmt.Printf("Changes in %q:\n", p.Name)
	fmt.Printf("  +%d added  ~%d changed  -%d removed  =%d unchanged\n",
		c.Added, c.Changed, c.Removed, c.Unchanged)
}

func diffCmd() *cobra.Command {
	var reveal bool
	cmd := &cobra.Command{
		Use:   "diff <projectA> <projectB>",
		Short: "Compare the environments of two projects",
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
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
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

func gapsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "gaps",
		Short: "Find keys that some projects have but others are missing",
		RunE: func(cmd *cobra.Command, args []string) error {
			v, pw, err := openVault(cmd.Context())
			if err != nil {
				return err
			}
			clearBytes(pw)
			gaps := analysis.FindGaps(v)
			if len(gaps) == 0 {
				fmt.Println("No gaps — every shared key is present in every project.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
			fmt.Fprintln(w, "PROJECT\tMISSING KEY\tDEFINED IN")
			for _, g := range gaps {
				fmt.Fprintf(w, "%s\t%s\t%v\n", g.Project, g.Key, g.DefinedIn)
			}
			return w.Flush()
		},
	}
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
	return maybeMask(val, reveal)
}
