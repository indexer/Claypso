---
type: Business Rule
title: Audit Log
description: Reveal-class and control operations append hash-chained events to <vault>.audit.log.
---

## Rule

internal/audit. Every gate decision (get/diff/drift --reveal, plain pull, exports, sync, rekey) and control op (lockdown on/off) appends a JSON line recording time, op, project@env, key count, outcome (ok/denied), user, parent process name, TTY and CALYPSO_UNATTENDED state. Values and passphrases are never logged. Lines form a SHA-256 hash chain (each event carries prev + hash); calypso audit verify walks it and reports the first break. File lives next to the vault (<vault>.audit.log, 0600), appends are flock-guarded, failures warn but never block the operation. CALYPSO_AUDIT=0 disables. Denied attempts are logged too — an agent probing --reveal leaves a trace. Strict denials and trusted-operation/broker runs record only opaque operation IDs, never labels, endpoints, secret names, or values.

## Relationships

- Applies to: [Vault](/concepts/vault.md)
- Depends on: [Reveal Gate](/rules/reveal-gate.md)
