---
type: Business Rule
title: Lockdown Mode
description: Vault-stored flag that disables reveal-class commands regardless of TTY or env vars.
---

## Rule

Vault.Lockdown (JSON field, schema v2 only) is toggled by calypso lockdown on/off/status. "on" works with any unlock method. "off" deliberately ignores the keychain and CALYPSO_PASSPHRASE — it requires the master passphrase typed at an interactive TTY, proving a human is present. While on, guardReveal rejects reveal-class ops even with CALYPSO_UNATTENDED=1; pull --safe and pull -- cmd (with scrubbing) keep working so agents stay productive. vault downgrade to schema v1 is blocked while lockdown is on. Deploy exemption: `lockdown on --allow-unattended` (Vault.LockdownUnattendedOK) keeps reveal-class ops working only where CALYPSO_UNATTENDED=1 is set — for CI vault copies; it weakens lockdown to reveal-gate strength for any process willing to set that variable. vault rekey is hard-blocked under lockdown regardless of the exemption.

## Relationships

- Applies to: [Vault](/concepts/vault.md)
- Depends on: [Reveal Gate](/rules/reveal-gate.md)
