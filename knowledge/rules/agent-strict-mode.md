---
type: Business Rule
title: Agent Strict Mode
description: Vault-stored state that blocks every path exposing credential names or values to agent-controlled files, output, commands, or configuration.
---

## Rule

`calypso strict on` stores AgentStrict in the encrypted vault and is safe to enable headlessly. Disabling it requires the master passphrase entered directly at a TTY. While enabled, owner and compatibility commands are rejected centrally after vault open: set/get/unset, push/pull, diff/gaps/drift/exposure/dashboard, imports/exports/backups, sync, rekey/downgrade, and Trusted Operation mutation. Allowed commands are metadata-only list/status/audit plus Trusted Operation invocation and broker serving. Strict mode is schema v3-only and blocks downgrade so an older representation can never silently discard the boundary. Before activation, every readable registered .env is checked for unsynced edits and replaced atomically with a name-free strict sentinel; activation refuses rather than overwrite unsynced work. AgentStrict requires hard Lockdown without an unattended exemption.

## Confidentiality boundary

Strict mode never places vault variables in an agent-controlled environment or .env file. Brokered operations execute fixed HTTP requests inside Calypso's hardened, non-dumpable process and return only operation ID, success/failure, HTTP status, and duration. Secret key names, values, upstream headers, response bodies, redirects, and transport error details are never returned.

## Relationships

- Applies to: [Vault](/concepts/vault.md)
- Depends on: [Lockdown Mode](/rules/lockdown-mode.md)
- Enforced by: [Operation Broker](/components/operation-broker.md)
