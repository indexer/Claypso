---
type: Application Component
title: Vault Component
description: internal/vault — encrypted vault, environments, file locking, backups.
---

## Responsibility

Loads/saves the encrypted vault via internal/crypto, enforces atomic fsync'd writes, flock(2) via internal/lockfile, and auto-backups. internal/project parses/serializes .env (multiline aware) and holds values as memguard-backed project.Secret. internal/analysis provides diff, gaps, and drift; internal/dashboard serves the read-only, value-free web overview on 127.0.0.1.

## Relationships

- Enforced by: [Vault Encryption Scheme](/rules/vault-encryption-scheme.md)
