---
type: Application Component
title: Keychain Component
description: internal/keychain — macOS Keychain (security) and Linux libsecret (secret-tool) backends.
---

## Responsibility

Stores only the master passphrase, never env values. keychain save/status/forget manage it. On wrong stored passphrase calypso forgets the entry and falls back to prompting. Windows unsupported; use CALYPSO_PASSPHRASE. Purpose: unattended agents and scripts can run calypso without a TTY prompt that would hang them.

## Relationships

- Depends on: [Passphrase Handling](/rules/passphrase-handling.md)
