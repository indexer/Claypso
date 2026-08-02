---
type: Business Rule
title: Vault Encryption Scheme
description: Argon2id (64 MB, 3 iterations, 4 threads) key derivation plus NaCl secretbox authenticated encryption.
---

## Rule

Fresh salt and nonce on every save; no key reuse. Derived key held in memguard locked memory (mlock'd, guarded, wiped). Core dumps and ptrace disabled at startup so secrets cannot reach disk. Vault and .env files written with 0600 permissions. Exports and auto-backups are byte-identical encrypted blobs requiring the master passphrase.

## Relationships

- Applies to: [Vault](/concepts/vault.md)
