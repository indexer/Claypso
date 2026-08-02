package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yemon/calypso/internal/project"
	"github.com/yemon/calypso/internal/vault"
)

// envCmd groups env-management subcommands. Most day-to-day operations don't
// need these — `add`, `remove`, `set`, `get` all accept `name@env` directly.
// `env list` and `env copy` are dedicated tools that don't fit cleanly into
// any of the existing top-level commands.
func envCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "env",
		Short: "List and manage environments within a project",
	}
	cmd.AddCommand(envListCmd(), envCopyCmd())
	return cmd
}

func envListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <project>",
		Short: "List the environments registered for a project",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, pw, err := openMetadataVault(cmd.Context(), "env list")
			if err != nil {
				return err
			}
			clearBytes(pw)
			p, err := v.Project(args[0])
			if err != nil {
				return err
			}
			w := newTabWriter()
			fmt.Fprintln(w, "ENV\tVARS\tUPDATED\tPATH")
			for _, en := range p.EnvNames() {
				e := p.Envs[en]
				fmt.Fprintf(w, "%s\t%d\t%s\t%s\n", e.Name, len(e.Vars), e.UpdatedAt, e.Path)
			}
			return w.Flush()
		},
	}
}

// envCopyCmd creates a new env by copying every variable from another env of
// the same project. Useful for bootstrapping `production` from `staging`.
// The new env needs its own .env path because it lives at a different
// location on disk.
func envCopyCmd() *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "copy <project@src> <dst>",
		Short: "Clone an env's variables into a new env of the same project",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			srcSpec, err := vault.ParseSpec(args[0])
			if err != nil {
				return err
			}
			if srcSpec.Env == "" {
				return fmt.Errorf("source must include an env: `%s@<env>`", srcSpec.Project)
			}
			dstName := args[1]
			if err := vault.ValidateEnvName(dstName); err != nil {
				return err
			}
			if path == "" {
				return fmt.Errorf("--path is required so the new env knows where to read/write its .env")
			}

			ctx := cmd.Context()
			v, pw, err := openOwnerVault(ctx, "env copy")
			if err != nil {
				return err
			}
			defer clearBytes(pw)

			p, src, err := v.ResolveEnv(args[0])
			if err != nil {
				return err
			}
			dst, err := v.AddEnvToProject(p.Name, dstName, path)
			if err != nil {
				return err
			}
			// Deep-copy vars so later edits to src don't leak into dst.
			dst.Vars = make([]project.Var, len(src.Vars))
			copy(dst.Vars, src.Vars)
			dst.InvalidateIndex()
			v.Touch(p.Name, dst.Name)

			if err := saveAndWarn(ctx, v, pw); err != nil {
				return err
			}
			fmt.Printf("Copied %d variable(s) from %s@%s into %s@%s → %s\n",
				len(dst.Vars), p.Name, src.Name, p.Name, dst.Name, dst.Path)
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", "", "path to the new env's .env file (required)")
	return cmd
}
