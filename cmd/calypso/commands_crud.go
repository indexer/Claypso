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

func addCmd() *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "add <project>",
		Short: "Register a project and the location of its .env file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if path == "" {
				path = ".env"
			}
			ctx := cmd.Context()
			v, pw, err := openVault(ctx)
			if err != nil {
				return err
			}
			defer clearBytes(pw)
			if _, err := v.AddProject(name, path); err != nil {
				return err
			}
			if err := v.Save(ctx, vaultPath, pw); err != nil {
				return err
			}
			fmt.Printf("Registered %q → %s\n", name, path)
			fmt.Printf("Tip: `calypso push %s` to import an existing .env, or `calypso set %s KEY=value`.\n", name, name)
			return nil
		},
	}
	cmd.Flags().StringVar(&path, "path", "", "path to the project's .env file (default ./.env)")
	return cmd
}

func setCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <project> KEY=value [KEY=value ...]",
		Short: "Set one or more variables in the vault",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			ctx := cmd.Context()
			v, pw, err := openVault(ctx)
			if err != nil {
				return err
			}
			defer clearBytes(pw)
			p, err := v.Project(name)
			if err != nil {
				return err
			}
			for _, kv := range args[1:] {
				eq := strings.Index(kv, "=")
				if eq < 0 {
					return fmt.Errorf("invalid pair %q, expected KEY=value", kv)
				}
				p.Set(strings.TrimSpace(kv[:eq]), kv[eq+1:])
			}
			v.Touch(name)
			if err := v.Save(ctx, vaultPath, pw); err != nil {
				return err
			}
			fmt.Printf("Updated %d variable(s) in %q.\n", len(args)-1, name)
			return nil
		},
	}
}

func getCmd() *cobra.Command {
	var reveal bool
	cmd := &cobra.Command{
		Use:   "get <project> [KEY]",
		Short: "Show variables (masked unless --reveal)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			v, pw, err := openVault(cmd.Context())
			if err != nil {
				return err
			}
			clearBytes(pw)
			p, err := v.Project(args[0])
			if err != nil {
				return err
			}
			if len(args) == 2 {
				val, ok := p.Get(args[1])
				if !ok {
					return fmt.Errorf("key %q not found in %q", args[1], args[0])
				}
				fmt.Println(maybeMask(val, reveal))
				return nil
			}
			for _, kv := range p.Vars {
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
		Use:   "unset <project> KEY [KEY ...]",
		Short: "Remove variables from the vault",
		Args:  cobra.MinimumNArgs(2),
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
			removed := 0
			for _, k := range args[1:] {
				if p.Unset(k) {
					removed++
				}
			}
			v.Touch(args[0])
			if err := v.Save(ctx, vaultPath, pw); err != nil {
				return err
			}
			fmt.Printf("Removed %d variable(s) from %q.\n", removed, args[0])
			return nil
		},
	}
}

func removeCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "remove <project>",
		Short: "Unregister a project from the vault (does not delete its .env)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			v, pw, err := openVault(ctx)
			if err != nil {
				return err
			}
			defer clearBytes(pw)
			if _, err := v.Project(args[0]); err != nil {
				return err
			}
			if !force {
				ok, err := confirmYesNo(os.Stderr, fmt.Sprintf("Remove %q from the vault?", args[0]), false)
				if err != nil {
					return err
				}
				if !ok {
					return fmt.Errorf("remove cancelled")
				}
			}
			if err := v.RemoveProject(args[0]); err != nil {
				return err
			}
			if err := v.Save(ctx, vaultPath, pw); err != nil {
				return err
			}
			fmt.Printf("Removed %q from the vault.\n", args[0])
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation prompt")
	return cmd
}
