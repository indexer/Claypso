---
type: Business Rule
title: Reveal Gate
description: Reveal-class commands require an interactive terminal or explicit CALYPSO_UNATTENDED=1.
---

## Rule

get --reveal, diff --reveal, drift --reveal, plain pull (real values to disk), export, and vault export-project call guardReveal (cmd/calypso/revealgate.go). When stdin is not a TTY the command fails unless CALYPSO_UNATTENDED=1 is set (CI escape hatch). Headless AI agents therefore cannot reveal by default. Masked ops (list, get, pull --safe, pull -- cmd, drift counts, gaps) are never gated.

## Relationships

- Applies to: [Environment Variable](/concepts/environment-variable.md)
