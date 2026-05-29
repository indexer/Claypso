package main

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/yemon/calypso/internal/keychain"
)

func keychainCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "keychain",
		Short: "Manage OS keychain storage for the master passphrase",
		Long: `Store and retrieve the vault master passphrase from the OS keychain.

Supported backends:
  Linux:   libsecret (apt install libsecret-tools)
  macOS:   Keychain (built-in)

Subcommands:
  save     Prompt for passphrase and store it in the keychain
  forget   Remove the passphrase from the keychain
  status   Check whether a passphrase is stored`,
	}

	cmd.AddCommand(
		keychainSaveCmd(),
		keychainForgetCmd(),
		keychainStatusCmd(),
	)
	return cmd
}

func keychainSaveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "save",
		Short: "Store the master passphrase in the OS keychain",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !keychain.Available() {
				return fmt.Errorf("%w", keychain.ErrNotAvailable)
			}
			pw, _, err := promptPassphrase("Master passphrase: ")
			if err != nil {
				return err
			}
			defer clearBytes(pw)
			if err := keychain.Store(pw); err != nil {
				return fmt.Errorf("keychain store failed: %w", err)
			}
			fmt.Println("Passphrase stored in keychain.")
			return nil
		},
	}
}

func keychainForgetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "forget",
		Short: "Remove the master passphrase from the OS keychain",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := keychain.Forget(); err != nil {
				return fmt.Errorf("keychain forget failed: %w", err)
			}
			fmt.Println("Passphrase removed from keychain.")
			return nil
		},
	}
}

func keychainStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Check whether a passphrase is stored in the keychain",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !keychain.Available() {
				fmt.Println("Keychain backend: not available")
				return nil
			}
			_, err := keychain.Retrieve()
			if err != nil {
				fmt.Println("Keychain backend: available, but no passphrase stored")
				return nil
			}
			fmt.Println("Keychain backend: available, passphrase is stored")
			return nil
		},
	}
}
