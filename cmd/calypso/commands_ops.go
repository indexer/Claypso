package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"
	"github.com/yemon/calypso/internal/analysis"
	"github.com/yemon/calypso/internal/project"
)

func listCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List all registered projects and their environments",
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
			w := newTabWriter()
			fmt.Fprintln(w, "PROJECT\tENV\tVARS\tUPDATED\tPATH")
			for _, name := range v.Names() {
				p := v.Projects[name]
				for _, en := range p.EnvNames() {
					e := p.Envs[en]
					fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\n", p.Name, e.Name, len(e.Vars), e.UpdatedAt, e.Path)
				}
			}
			return w.Flush()
		},
	}
}

func pullCmd() *cobra.Command {
	var safe, example, force bool
	cmd := &cobra.Command{
		Use:   "pull <project[@env]> [-- command...]",
		Short: "Write the env's values out to its .env file",
		Long: `pull writes variables from the vault to the env's .env file.

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
			spec := args[0]
			wipeCmd := args[1:]

			v, pw, err := openVault(cmd.Context())
			if err != nil {
				return err
			}
			clearBytes(pw)
			_, e, err := v.ResolveEnv(spec)
			if err != nil {
				return err
			}

			switch {
			case example:
				if err := project.WriteExampleEnvFile(e.Path, e.Vars); err != nil {
					return err
				}
				fmt.Printf("Wrote %d key(s) (empty) to %s\n", len(e.Vars), e.Path)
				return nil
			case safe:
				if err := project.WriteSafeEnvFile(e.Path, e.Vars); err != nil {
					return err
				}
				fmt.Printf("Wrote %d safe variable(s) to %s\n", len(e.Vars), e.Path)
				return nil
			case len(wipeCmd) > 0:
				return wipeAndRun(e.Path, e.Vars, wipeCmd)
			default:
				if !force {
					ok, err := confirmEnvOverwrite(e.Path, e.Vars)
					if err != nil {
						return err
					}
					if !ok {
						return fmt.Errorf("pull cancelled")
					}
				}
				if err := project.WriteEnvFile(e.Path, e.Vars); err != nil {
					return err
				}
				fmt.Printf("Wrote %d variable(s) to %s\n", len(e.Vars), e.Path)
				return nil
			}
		},
	}
	cmd.Flags().BoolVarP(&safe, "safe", "s", false, "write masked values (****) for LLM/teammate sharing")
	cmd.Flags().BoolVarP(&example, "example", "e", false, "write empty values for .env.example")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "overwrite the .env without confirming unsynced local edits")
	return cmd
}

// confirmEnvOverwrite checks whether the on-disk .env at path holds real
// values not yet in the vault (unsynced local edits that a real-value pull
// would silently destroy). If so it prints a diff summary and asks for
// confirmation. A missing file, a fully masked safe file, or a file that
// matches the vault returns true without prompting — those are the normal,
// non-destructive cases. Unattended runs (CALYPSO_PASSPHRASE set) auto-confirm.
func confirmEnvOverwrite(path string, vaultVars []project.Var) (bool, error) {
	existing, err := project.ReadEnvFile(path)
	if err != nil {
		return true, nil // no readable .env to clobber
	}
	if !hasUnsyncedEdits(vaultVars, existing) {
		return true, nil
	}
	c := analysis.DiffCounts(vaultVars, existing)
	fmt.Fprintf(os.Stderr, "%s has local changes not in the vault (relative to vault: +%d ~%d -%d).\n",
		path, c.Added, c.Changed, c.Removed)
	return confirmYesNo(os.Stderr, "Overwrite .env with vault values?", false)
}

// hasUnsyncedEdits reports whether the on-disk vars contain a real (non-masked)
// key/value that the vault doesn't already have. Masked placeholder values are
// ignored so the documented `pull --safe` → `pull` restore round-trip never
// prompts.
func hasUnsyncedEdits(vaultVars, fileVars []project.Var) bool {
	inVault := make(map[string]string, len(vaultVars))
	for _, v := range vaultVars {
		inVault[v.Key] = v.Value
	}
	for _, fv := range fileVars {
		if fv.Value == project.SafePlaceholder {
			continue
		}
		if vv, ok := inVault[fv.Key]; !ok || vv != fv.Value {
			return true
		}
	}
	return false
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
	c.Env = stripSensitiveEnv(os.Environ())
	// Put the child in its own process group on Unix so signals (Ctrl+C)
	// reach it directly rather than us. Windows has no equivalent and the
	// helper is a no-op there — see wipe_procgroup_*.go.
	setProcessGroup(c)

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
		Use:   "push <project[@env]>",
		Short: "Read the env's .env file back into the vault",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			v, pw, err := openVault(ctx)
			if err != nil {
				return err
			}
			defer clearBytes(pw)
			p, e, err := v.ResolveEnv(args[0])
			if err != nil {
				return err
			}
			vars, err := project.ReadEnvFile(e.Path)
			if err != nil {
				return fmt.Errorf("reading %s: %w", e.Path, err)
			}
			// The parser doesn't enforce key syntax or value size, so a
			// hand-edited .env can carry names `set` would reject or huge values.
			// Validate before ingesting so the vault never stores keys that
			// won't round-trip back to a .env, or unbounded values.
			if err := project.ValidateVars(vars); err != nil {
				return fmt.Errorf("%s: %w", e.Path, err)
			}
			// A .env may list a key more than once; keep last-wins so the vault
			// never stores duplicates.
			vars = project.DedupeKeys(vars)

			if !force && len(e.Vars) > 0 {
				showPushDiff(p.Name, e, vars)
				ok, err := confirmYesNo(os.Stderr, "Overwrite vault values with .env?", false)
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("push cancelled")
				}
			}

			e.Vars = vars
			e.InvalidateIndex()
			v.Touch(p.Name, e.Name)
			if err := saveAndWarn(ctx, v, pw); err != nil {
				return err
			}
			fmt.Printf("Imported %d variable(s) from %s into %s@%s.\n", len(vars), e.Path, p.Name, e.Name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation prompt")
	return cmd
}

func showPushDiff(projectName string, e *project.Environment, incoming []project.Var) {
	c := analysis.DiffCounts(e.Vars, incoming)
	fmt.Printf("Changes in %s@%s:\n", projectName, e.Name)
	fmt.Printf("  +%d added  ~%d changed  -%d removed  =%d unchanged\n",
		c.Added, c.Changed, c.Removed, c.Unchanged)
}
