package main

import (
	"context"
	"fmt"
	"os"

	"github.com/yemon/calypso/internal/vault"
)

type vaultAccess int

const (
	accessOwner vaultAccess = iota
	accessMetadata
	accessOperation
)

// openVaultFor is the central strict-mode capability check. Security-sensitive
// commands must not call openVault directly: tests enforce that this wrapper is
// the only command-layer entry point.
func openVaultFor(ctx context.Context, access vaultAccess, op string) (*vault.Vault, []byte, error) {
	v, pw, err := openVault(ctx)
	if err != nil {
		return nil, nil, err
	}
	if v.AgentStrict && access == accessOwner {
		err := fmt.Errorf("%s is blocked: vault is in agent strict mode; run only metadata commands or a trusted operation", op)
		recordAudit("strict-denied", op, 0, err)
		v.Close()
		clearBytes(pw)
		return nil, nil, err
	}
	return v, pw, nil
}

func openOwnerVault(ctx context.Context, op string) (*vault.Vault, []byte, error) {
	return openVaultFor(ctx, accessOwner, op)
}

func openMetadataVault(ctx context.Context, op string) (*vault.Vault, []byte, error) {
	return openVaultFor(ctx, accessMetadata, op)
}

func openOperationVault(ctx context.Context, op string) (*vault.Vault, []byte, error) {
	return openVaultFor(ctx, accessOperation, op)
}

// guardExistingOwner protects file-level replacement and keychain mutation
// commands that historically did not decrypt the vault. A missing vault has no
// strict state to preserve and is allowed for bootstrap/recovery.
func guardExistingOwner(ctx context.Context, op string) error {
	if _, err := os.Stat(vaultPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	v, pw, err := openOwnerVault(ctx, op)
	if err != nil {
		return err
	}
	v.Close()
	clearBytes(pw)
	return nil
}
