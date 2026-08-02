package main

import (
	"encoding/base64"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/yemon/calypso/internal/crypto"
	"github.com/yemon/calypso/internal/vault"
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
			v, pw, err := openOwnerVault(cmd.Context(), "export")
			if err != nil {
				return err
			}
			clearBytes(pw)
			defer v.Close()
			gateErr := guardReveal(v.Lockdown, v.LockdownUnattendedOK, "export")
			recordAudit("export", "", 0, gateErr)
			if gateErr != nil {
				return gateErr
			}
			blob, err := os.ReadFile(vaultPath)
			if err != nil {
				return fmt.Errorf("vault not found at %s: %w", vaultPath, err)
			}

			if base64Out {
				fmt.Println(base64.StdEncoding.EncodeToString(blob))
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

// importCmd defaults to the original behavior: blind-copy the encrypted
// blob over the current vault file (no passphrase needed at import time).
// Pass --merge to enable partial-export detection — that path decrypts the
// blob with the master passphrase and, if it's a partial export, merges
// the project into the existing vault instead of replacing it.
//
// Splitting the two modes (rather than auto-detecting) keeps the
// no-passphrase blind copy working in unattended pipelines and avoids
// surprising the user with a passphrase prompt for a full-vault restore.
func importCmd() *cobra.Command {
	var base64In, merge, force bool
	cmd := &cobra.Command{
		Use:   "import <path>",
		Short: "Restore a vault from an export (or merge a per-project export with --merge)",
		Long: `import accepts two shapes of encrypted blob:

  - Default: full-vault restore. The blob is written verbatim over the
    current vault file. No passphrase needed at import time; the next
    command that opens the vault will validate it.

  - With --merge: partial-export merge. The blob is decrypted with the
    master passphrase and, if it looks like a 'vault export-project'
    output, the one project it contains is merged into the existing
    vault. Refuses to overwrite an existing project of the same name
    unless --force is set.

Pass --base64 to read a base64-encoded blob from the named file (useful
when restoring from a password manager or git note).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := guardExistingOwner(cmd.Context(), "import"); err != nil {
				return err
			}
			blob, err := readImportBlob(args[0], base64In)
			if err != nil {
				return err
			}
			if merge {
				return mergeImport(cmd, blob, force)
			}
			return fullVaultImport(blob)
		},
	}
	cmd.Flags().BoolVarP(&base64In, "base64", "b", false, "read base64-encoded input from the file")
	cmd.Flags().BoolVar(&merge, "merge", false, "treat input as a partial export and merge into the current vault")
	cmd.Flags().BoolVarP(&force, "force", "f", false, "with --merge, replace an existing project of the same name")
	return cmd
}

func readImportBlob(path string, isBase64 bool) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading import file: %w", err)
	}
	if !isBase64 {
		return raw, nil
	}
	dec, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil {
		return nil, fmt.Errorf("invalid base64: %w", err)
	}
	return dec, nil
}

// fullVaultImport is the unattended-friendly path: blind-copy the encrypted
// blob over the current vault file. No passphrase is needed here; the next
// command that opens the vault will validate it.
func fullVaultImport(blob []byte) error {
	if err := vault.ImportRawBlob(vaultPath, blob); err != nil {
		return fmt.Errorf("writing imported vault: %w", err)
	}
	fmt.Printf("Imported full vault to %s (%d bytes)\n", vaultPath, len(blob))
	return nil
}

// mergeImport is the --merge path: prompt for the passphrase, decrypt the
// blob, confirm it's a partial export, then merge its project into the
// current vault. The Save it triggers auto-backs-up the pre-merge vault.
func mergeImport(cmd *cobra.Command, blob []byte, force bool) error {
	pw, _, err := promptPassphrase("Master passphrase: ")
	if err != nil {
		return err
	}
	defer clearBytes(pw)

	plain, err := crypto.Decrypt(pw, blob)
	if err != nil {
		return fmt.Errorf("decrypt import: %w", err)
	}
	defer crypto.Wipe(plain)
	if !vault.IsPartialExport(plain) {
		return fmt.Errorf("--merge expects a partial-export blob (from `vault export-project`); use plain `import` for full-vault restores")
	}

	ctx := cmd.Context()
	v, err := vault.Load(ctx, vaultPath, pw)
	if err != nil {
		return fmt.Errorf("load current vault: %w", err)
	}
	if v.AgentStrict {
		return fmt.Errorf("import --merge is blocked: vault is in agent strict mode")
	}
	tmp, err := writeTempFromBlob(blob)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	name, err := v.ImportProject(pw, tmp, force)
	if err != nil {
		return err
	}
	if err := saveAndWarn(ctx, v, pw); err != nil {
		return err
	}
	fmt.Printf("Merged project %q from partial import.\n", name)
	return nil
}

// writeTempFromBlob stages the encrypted blob in a tempfile so
// vault.ImportProject (which reads from a path) can consume it without
// changing its signature.
func writeTempFromBlob(blob []byte) (string, error) {
	f, err := os.CreateTemp("", "calypso-import-*.enc")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(blob); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
