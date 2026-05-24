package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// vaultExportProjectCmd writes a single project (or one of its envs) as
// an encrypted partial-export blob. The recipient imports with the same
// `calypso import` command — it auto-detects partial vs full-vault input.
func vaultExportProjectCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "export-project <project[@env]> <path>",
		Short: "Export one project (or one env) as an encrypted shareable blob",
		Long: `export-project writes a smaller alternative to a full vault export:

  - 'myapp'         exports every env of myapp
  - 'myapp@prod'    exports just the prod env

The output file is encrypted with the same passphrase as the vault (so
the recipient needs that passphrase). 'calypso import' on the other end
auto-detects partial vs full-vault blobs.

Use this for sharing a single project's setup with a teammate or for a
targeted backup of just the env you care about.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			spec, dst := args[0], args[1]
			ctx := cmd.Context()
			v, pw, err := openVault(ctx)
			if err != nil {
				return err
			}
			defer clearBytes(pw)
			if err := v.ExportProject(ctx, spec, dst, pw); err != nil {
				return err
			}
			fmt.Printf("Exported %s → %s\n", spec, dst)
			return nil
		},
	}
	_ = force
	return cmd
}
