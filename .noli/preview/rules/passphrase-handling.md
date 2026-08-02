---
type: Business Rule
title: Passphrase Handling
description: Master passphrase is the single key; read without echo, zeroed after use, never recoverable.
---

## Rule

Prompted in terminal raw mode without echo, SIGINT-safe, zeroed in memory after every operation, 3 retries with increasing delay. Non-interactive use: CALYPSO_PASSPHRASE env var (legacy ENVHUB_PASSPHRASE honored with deprecation warning); when set it bypasses the keychain entirely. OS keychain (macOS security / Linux secret-tool) can cache the passphrase — this moves the security boundary to the OS user account: any process running as the user can read it. Rotation: `vault rekey` re-encrypts under a new passphrase (interactive double-prompt, or CALYPSO_NEW_PASSPHRASE plus CALYPSO_UNATTENDED=1 for CI); keeps <vault>.pre-rekey.bak (old passphrase), refreshes the keychain entry in interactive runs, and old auto-backups/exports still need the old passphrase. CALYPSO_NEW_PASSPHRASE is stripped from pull -- cmd children like the other passphrase vars.

## Relationships

- Applies to: [Vault](/concepts/vault.md)
- Uses: [Keychain Component](/components/keychain-component.md)
