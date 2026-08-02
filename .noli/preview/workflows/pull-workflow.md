---
type: Workflow
title: Pull Workflow
description: Export vault to .env in one of three modes — real, safe (masked), or wipe (run command then shred).
---

## Steps

1) pull <spec> writes real values to the env's path. 2) pull --safe writes **** placeholders with a "Managed by calypso. Safe mode" header. 3) pull <spec> -- cmd writes real values, executes cmd, and always overwrites the .env with masked placeholders afterwards, even on command failure. 4) pull --example writes empty values for a committable .env.example.

## Relationships

- Uses: [Vault Component](/components/vault-component.md)
