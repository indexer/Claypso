---
type: Application Component
title: CLI Component
description: Cobra-based CLI in cmd/calypso with 20+ commands.
---

## Responsibility

Commands grouped in commands_*.go files (crud, vars, ops, analysis, drift, env, keychain, transfer, vault, backups, verify). openVault in helpers tries keychain first, then CALYPSO_PASSPHRASE, then prompts. harden_linux.go disables core dumps/ptrace at startup; wipe_procgroup_*.go implements the pull -- cmd shred path.

## Relationships

- Uses: [Keychain Component](/components/keychain-component.md)
- Uses: [Vault Component](/components/vault-component.md)
