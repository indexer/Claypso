---
type: Application Component
title: Platform Sync
description: calypso sync fly|vercel|k8s pushes an env's real values into a platform secret store.
---

## Responsibility

cmd/calypso/commands_sync.go. Shells out to the platform CLI — flyctl secrets import (KEY=VALUE lines on stdin), vercel env add (one call per key, value on stdin, --force upsert), kubectl apply -f - (Secret manifest built in memory, values base64). Real values never touch disk or argv. Child stdout/stderr run through the scrubber so a CLI echoing a value prints <concealed>. Passphrase env vars are stripped from the child. Reveal-class gated (lockdown --allow-unattended exemption applies, fitting CI) and audited. --dry-run prints masked payload. Fly values containing newlines are rejected (line format cannot carry them); k8s names are RFC-1123 sanitized (lowercase, _ becomes -).

## Relationships

- Depends on: [Reveal Gate](/rules/reveal-gate.md)
- Uses: [Vault Component](/components/vault-component.md)
- Uses: [Output Scrubbing](/rules/output-scrubbing.md)
