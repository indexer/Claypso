package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/yemon/calypso/internal/project"
)

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
				key := strings.TrimSpace(kv[:eq])
				if !project.ValidKey(key) {
					return fmt.Errorf("invalid key %q: keys must start with a letter or _ and contain only letters, digits, or _ (max %d chars)", key, project.MaxKeyLen)
				}
				val := kv[eq+1:]
				if len(val) > project.MaxValueLen {
					return fmt.Errorf("value for %q is too large: %d bytes (max %d)", key, len(val), project.MaxValueLen)
				}
				e.Set(key, val)
			}
			v.Touch(p.Name, e.Name)
			if err := saveAndWarn(ctx, v, pw); err != nil {
				return err
			}
			fmt.Printf("Updated %d variable(s) in %s@%s.\n", len(args)-1, p.Name, e.Name)
			return nil
		},
	}
}

func getCmd() *cobra.Command {
	var reveal, hint bool
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
				fmt.Println(maybeMask(val, reveal, hint))
				return nil
			}
			for _, kv := range e.Vars {
				fmt.Printf("%s=%s\n", kv.Key, maybeMask(kv.Value, reveal, hint))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&reveal, "reveal", false, "show actual values instead of masking")
	cmd.Flags().BoolVar(&hint, "hint", false, "mask but reveal first/last 2 chars as an identification aid")
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
			if err := saveAndWarn(ctx, v, pw); err != nil {
				return err
			}
			fmt.Printf("Removed %d variable(s) from %s@%s.\n", removed, p.Name, e.Name)
			return nil
		},
	}
}
