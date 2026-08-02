package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/yemon/calypso/internal/crypto"
	"github.com/yemon/calypso/internal/keychain"
	"github.com/yemon/calypso/internal/vault"
)

const maxRetries = 3

// vaultPath is set via --vault flag.
var vaultPath string

func rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "calypso",
		Short: "Local, encrypted manager for your projects' .env files",
		Long: `calypso keeps every project's environment in one encrypted vault on
your machine. Edit values centrally, sync them out to each project's .env
on demand, and see all your environments at a glance. Nothing ever leaves
your machine.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	def, err := vault.DefaultPath()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: cannot determine home directory for vault: %v\n", err)
		def = "calypso.enc"
	}
	root.PersistentFlags().StringVar(&vaultPath, "vault", def, "path to the encrypted vault file")

	root.AddCommand(
		initCmd(), addCmd(), setCmd(), getCmd(), unsetCmd(),
		listCmd(), pullCmd(), pushCmd(), diffCmd(), gapsCmd(),
		removeCmd(), dashboardCmd(), completionCmd(),
		exportCmd(), importCmd(), keychainCmd(), versionCmd(),
		envCmd(), vaultCmd(), driftCmd(), lockdownCmd(),
		exposureCmd(), agentCmd(), auditCmd(), syncCmd(),
		strictCmd(), operationCmd(), brokerCmd(),
	)
	root.Version = version
	return root
}

func main() {
	// Best-effort: disable core dumps / ptrace before any secret is in memory,
	// so a crash can't persist the vault's plaintext to disk.
	if err := hardenProcess(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: process hardening failed: %v\n", err)
	}
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// openVault loads the vault. Tries keychain first, then interactive prompt
// with retries. Auto-creates the vault if it doesn't exist.
func openVault(ctx context.Context) (*vault.Vault, []byte, error) {
	usingEnv := unattended()

	if !usingEnv && keychain.Available() {
		if v, pw, ok := tryKeychainUnlock(ctx); ok {
			return v, pw, nil
		}
	}

	pw, fromEnv, err := promptPassphrase("Master passphrase: ")
	if err != nil {
		return nil, nil, err
	}

	v, err := vault.Load(ctx, vaultPath, pw)
	if err == nil {
		if !fromEnv && keychain.Available() {
			offerKeychainSave(pw)
		}
		return v, pw, nil
	}
	if errors.Is(err, vault.ErrNotFound) {
		clearBytes(pw)
		return autoInit(ctx)
	}
	if err != crypto.ErrDecrypt {
		clearBytes(pw)
		return nil, nil, err
	}

	clearBytes(pw)
	if fromEnv {
		_, name, _ := passphraseFromEnv()
		return nil, nil, fmt.Errorf("wrong passphrase (from %s)", name)
	}
	return retryLoad(ctx)
}

// tryKeychainUnlock attempts vault decryption with the stored keychain passphrase.
func tryKeychainUnlock(ctx context.Context) (*vault.Vault, []byte, bool) {
	pw, err := keychain.Retrieve()
	if err != nil {
		return nil, nil, false
	}
	v, err := vault.Load(ctx, vaultPath, pw)
	if err == nil {
		return v, pw, true
	}
	clearBytes(pw)
	if err == crypto.ErrDecrypt {
		if ferr := keychain.Forget(); ferr != nil {
			fmt.Fprintf(os.Stderr, "Warning: clearing stale keychain entry failed: %v\n", ferr)
		}
	}
	return nil, nil, false
}

// retryLoad re-prompts up to maxRetries-1 times with increasing delay.
func retryLoad(ctx context.Context) (*vault.Vault, []byte, error) {
	for attempt := 1; attempt < maxRetries; attempt++ {
		time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
		prompt := fmt.Sprintf("Master passphrase (attempt %d/%d): ", attempt+1, maxRetries)
		pw, _, err := promptPassphrase(prompt)
		if err != nil {
			return nil, nil, err
		}
		v, err := vault.Load(ctx, vaultPath, pw)
		if err == nil {
			if keychain.Available() {
				offerKeychainSave(pw)
			}
			return v, pw, nil
		}
		clearBytes(pw)
		if err != crypto.ErrDecrypt {
			return nil, nil, err
		}
	}
	return nil, nil, fmt.Errorf("wrong passphrase after %d attempts", maxRetries)
}

// offerKeychainSave asks once whether to save the passphrase in the OS keychain.
func offerKeychainSave(pw []byte) {
	if unattended() {
		return
	}
	if stored, err := keychain.Retrieve(); err == nil {
		clearBytes(stored)
		return
	}
	ok, err := confirmYesNo(os.Stderr, "Save passphrase to OS keychain?", false)
	if err != nil || !ok {
		return
	}
	if err := keychain.Store(pw); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: keychain save failed: %v\n", err)
		return
	}
	fmt.Fprintln(os.Stderr, "Passphrase saved to keychain.")
}

// autoInit creates a new vault when none exists.
func autoInit(ctx context.Context) (*vault.Vault, []byte, error) {
	ok, err := confirmYesNo(os.Stderr, fmt.Sprintf("No vault found at %s.\nCreate one?", vaultPath), true)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, fmt.Errorf("vault creation cancelled; run `calypso init` to create one")
	}
	pw, err := readNewPassphrase()
	if err != nil {
		return nil, nil, err
	}
	v, err := vault.Init(ctx, vaultPath, pw)
	if err != nil {
		clearBytes(pw)
		return nil, nil, err
	}
	fmt.Printf("Vault created at %s\n", vaultPath)
	fmt.Println("Keep your passphrase safe — it cannot be recovered.")
	if keychain.Available() {
		offerKeychainSave(pw)
	}
	return v, pw, nil
}
