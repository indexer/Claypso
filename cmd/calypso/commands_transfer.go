package main

import (
	"encoding/base64"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func exportCmd() *cobra.Command {
	var base64Out bool
	cmd := &cobra.Command{
		Use:   "export [path]",
		Short: "Export the encrypted vault to a file or stdout (base64)",
		Long: `export copies the encrypted vault blob. Without --base64,
it writes the raw encrypted file to the given path.

With --path, the raw encrypted blob is written to that file.

Without a path and --base64, the vault is base64-encoded to stdout —
safe for copy-paste into git notes, password managers, or chat.

The exported file is still encrypted — you need the master passphrase
to use it on another machine via 'calypso import'.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			blob, err := os.ReadFile(vaultPath)
			if err != nil {
				return fmt.Errorf("vault not found at %s: %w", vaultPath, err)
			}

			if base64Out {
				encoded := base64.StdEncoding.EncodeToString(blob)
				fmt.Println(encoded)
				return nil
			}

			if len(args) == 0 {
				return fmt.Errorf("specify output path or use --base64 for stdout")
			}
			dest := args[0]
			if err := os.WriteFile(dest, blob, 0o600); err != nil {
				return fmt.Errorf("writing export: %w", err)
			}
			fmt.Printf("Exported to %s (%d bytes)\n", dest, len(blob))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&base64Out, "base64", "b", false, "output base64-encoded to stdout")
	return cmd
}

func importCmd() *cobra.Command {
	var base64In bool
	cmd := &cobra.Command{
		Use:   "import <path|-->",
		Short: "Import an encrypted vault export into the current vault location",
		Long: `import replaces the current vault with an exported one.

With a file path, the raw encrypted blob is read from that file.

With --base64, the blob is read from stdin as a base64-encoded string —
useful for restoring from a git note or password manager backup.

The imported vault uses whatever passphrase was set when it was exported.
If the passphrases differ, subsequent calypso commands will fail with
'wrong passphrase'.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var blob []byte
			var err error

			if base64In {
				input, err2 := os.ReadFile(args[0])
				if err2 != nil {
					return fmt.Errorf("reading base64 input: %w", err2)
				}
				blob, err = base64.StdEncoding.DecodeString(string(input))
				if err != nil {
					return fmt.Errorf("invalid base64: %w", err)
				}
			} else {
				blob, err = os.ReadFile(args[0])
				if err != nil {
					return fmt.Errorf("reading import file: %w", err)
				}
			}

			if err := os.WriteFile(vaultPath, blob, 0o600); err != nil {
				return fmt.Errorf("writing imported vault: %w", err)
			}
			fmt.Printf("Imported vault to %s (%d bytes)\n", vaultPath, len(blob))
			return nil
		},
	}
	cmd.Flags().BoolVarP(&base64In, "base64", "b", false, "read base64-encoded input instead of raw blob")
	return cmd
}
