---
type: Domain Entity
title: Vault
description: Single encrypted file holding every project's environment variables.
---

## Definition

The vault is one encrypted blob at ~/.calypso/vault.enc containing all projects, their environments, and their variables. It is always encrypted at rest (Argon2id key derivation + NaCl secretbox), written atomically (.tmp then rename, fsync'd), guarded by flock(2) advisory locking, and auto-backed up to <vault>.backups/ on every successful save (retention via CALYPSO_BACKUP_RETAIN, default 10). Schema v3 additionally stores Agent Strict Mode and encrypted Trusted Operation definitions; v1/v2 vaults migrate in memory and upgrade only when v3-only state is saved.

## Relationships

- Enforced by: [Vault Encryption Scheme](/rules/vault-encryption-scheme.md)
