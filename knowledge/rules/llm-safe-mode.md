---
type: Business Rule
title: LLM Safe Mode
description: .env on disk can be masked so AI coding agents see key names but never real values.
---

## Rule

calypso pull <spec> --safe writes the .env with **** placeholders (keys visible, values masked) before an LLM session; plain pull restores real values afterwards. calypso pull <spec> -- cmd writes real values, runs cmd, then ALWAYS overwrites .env with **** placeholders even on failure (wipe mode, process-group aware).

## Enforcement boundary

Three in-binary layers back this up: the Reveal Gate (reveal-class ops need an interactive TTY or CALYPSO_UNATTENDED=1), Lockdown Mode (vault-stored flag blocking reveal-class ops entirely), and Output Scrubbing (child stdout/stderr filtered in pull -- cmd runs). This is compatibility-mode accidental-leak protection, not strict agent isolation: arbitrary commands receiving real secrets can exfiltrate them. Agent Strict Mode closes that path by refusing every arbitrary secret-bearing command and permitting only brokered Trusted Operations.

## Relationships

- Applies to: [Environment Variable](/concepts/environment-variable.md)
- Enforced by: [Agent Strict Mode](/rules/agent-strict-mode.md)
- Enforced by: [Lockdown Mode](/rules/lockdown-mode.md)
- Enforced by: [Output Scrubbing](/rules/output-scrubbing.md)
- Enforced by: [Reveal Gate](/rules/reveal-gate.md)
- Uses: [Pull Workflow](/workflows/pull-workflow.md)
