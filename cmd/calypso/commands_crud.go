package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yemon/calypso/internal/vault"
)

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Create a new encrypted vault",
		RunE: func(cmd *cobra.Command, args []string) error {
			pw, err := readNewPassphrase()
			if err != nil {
				return err
			}
			defer clearBytes(pw)
			if _, err := vault.Init(cmd.Context(), vaultPath, pw); err != nil {
				return err
			}
			fmt.Printf("Vault created at %s\n", vaultPath)
			fmt.Println("Keep your passphrase safe — it cannot be recovered.")
			return nil
		},
	}
}

// addCmd handles two shapes:
//   - `add myapp --path ...`                    → creates project + initial env
//   - `add myapp@prod --path ...`               → adds env `prod` to an existing project
//
// With the bare form, --env names the initial env (default: "default").
func addCmd() *cobra.Command {
	var path, envFlag string
	cmd := &cobra.Command{
		Use:   "add <project[@env]>",
		Short: "Register a project, or add a new environment to one",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := vault.ParseSpec(args[0])
			if err != nil {
				return err
			}
			if path == "" {
				path = ".env"
			}
			ctx := cmd.Context()
			v, pw, err := openVault(ctx)
			if err != nil {
				return err
			}
			defer clearBytes(pw)

			// Existing project + @env → attach a new env.
			if _, exists := v.Projects[spec.Project]; exists {
				if spec.Env == "" {
					return fmt.Errorf("project %q already registered; to add an env use `add %s@<env> --path ...`",
						spec.Project, spec.Project)
				}
				e, err := v.AddEnvToProject(spec.Project, spec.Env, path)
				if err != nil {
					return err
				}
				if err := v.Save(ctx, vaultPath, pw); err != nil {
					return err
				}
				fmt.Printf("Added env %q to %q → %s\n", e.Name, spec.Project, e.Path)
				return nil
			}

			// New project.
			envName := spec.Env
			if envName == "" {
				envName = envFlag // may be "" → AddProject defaults to project.DefaultEnvName
			}
			_, e, err := v.AddProject(spec.Project, envName, path)
			if err != nil {
				return err
			}
			if err := v.Save(ctx, vaultPath, pw); err != nil {
				return err
			}
			fmt.Printf("Registered %q (env %q) → %s\n", spec.Project, e.Name, e.Path)
			fmt.Printf("Tip: `calypso push %s` to import an existing .env, or `calypso set %s KEY=value`.\n",
				spec.Project, spec.Project)
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", "", "path to the project's .env file (default ./.env)")
	cmd.Flags().StringVar(&envFlag, "env", "", "name of the initial env (default: \"default\")")
	return cmd
}

func setCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <project[@env]> KEY=value [KEY=value ...]",
		Short: "Set one or more variables in the vault",
		Args:  cobra.MinimumNArgs(2),
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
			for _, kv := range args[1:] {
				eq := strings.Index(kv, "=")
				if eq < 0 {
					return fmt.Errorf("invalid pair %q, expected KEY=value", kv)
				}
				e.Set(strings.TrimSpace(kv[:eq]), kv[eq+1:])
			}
			v.Touch(p.Name, e.Name)
			if err := v.Save(ctx, vaultPath, pw); err != nil {
				return err
			}
			fmt.Printf("Updated %d variable(s) in %s@%s.\n", len(args)-1, p.Name, e.Name)
			return nil
		},
	}
}

func getCmd() *cobra.Command {
	var reveal bool
	cmd := &cobra.Command{
		Use:   "get <project[@env]> [KEY]",
		Short: "Show variables (masked unless --reveal)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, pw, err := openVault(cmd.Context())
			if err != nil {
				return err
			}
			clearBytes(pw)
			_, e, err := v.ResolveEnv(args[0])
			if err != nil {
				return err
			}
			if len(args) == 2 {
				val, ok := e.Get(args[1])
				if !ok {
					return fmt.Errorf("key %q not found in %q", args[1], args[0])
				}
				fmt.Println(maybeMask(val, reveal))
				return nil
			}
			for _, kv := range e.Vars {
				fmt.Printf("%s=%s\n", kv.Key, maybeMask(kv.Value, reveal))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&reveal, "reveal", false, "show actual values instead of masking")
	return cmd
}

func unsetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unset <project[@env]> KEY [KEY ...]",
		Short: "Remove variables from the vault",
		Args:  cobra.MinimumNArgs(2),
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
			removed := 0
			for _, k := range args[1:] {
				if e.Unset(k) {
					removed++
				}
			}
			v.Touch(p.Name, e.Name)
			if err := v.Save(ctx, vaultPath, pw); err != nil {
				return err
			}
			fmt.Printf("Removed %d variable(s) from %s@%s.\n", removed, p.Name, e.Name)
			return nil
		},
	}
}

// removeCmd handles both:
//   - `remove myapp`        → unregister the whole project (and all envs)
//   - `remove myapp@prod`   → remove just env `prod` (refuses if it's the last env)
func removeCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "remove <project[@env]>",
		Short: "Unregister a project, or remove one environment from it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, err := vault.ParseSpec(args[0])
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			v, pw, err := openVault(ctx)
			if err != nil {
				return err
			}
			defer clearBytes(pw)
			if _, err := v.Project(spec.Project); err != nil {
				return err
			}

			target := spec.Project
			if spec.Env != "" {
				target = spec.Project + "@" + spec.Env
			}
			if !force {
				ok, err := confirmYesNo(os.Stderr, fmt.Sprintf("Remove %q from the vault?", target), false)
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("remove cancelled")
				}
			}

			if spec.Env != "" {
				if err := v.RemoveEnv(spec.Project, spec.Env); err != nil {
					return err
				}
				if err := v.Save(ctx, vaultPath, pw); err != nil {
					return err
				}
				fmt.Printf("Removed env %q from %q.\n", spec.Env, spec.Project)
				return nil
			}

			if err := v.RemoveProject(spec.Project); err != nil {
				return err
			}
			if err := v.Save(ctx, vaultPath, pw); err != nil {
				return err
			}
			fmt.Printf("Removed %q from the vault.\n", spec.Project)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation prompt")
	return cmd
}
