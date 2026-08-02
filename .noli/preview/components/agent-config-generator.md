---
type: Application Component
title: Agent Config Generator
description: calypso agent init writes agent-tool deny rules (claude-code, codex, opencode).
---

## Responsibility

cmd/calypso/commands_agent.go. claude-code: merges deny/allow Bash prefix rules into .claude/settings.json (or ~/.claude/settings.json with --global), denying owner/compatibility commands, unattended bypasses, and keychain readers while allowing broker invocation and metadata. opencode receives equivalent deny globs. codex has no per-command deny, so it receives an advisory AGENTS.md policy permitting only broker invocation and metadata plus a shell_environment_policy snippet excluding every passphrase and unattended variable. All writers merge and dedupe. Agent Strict Mode and OS-separated broker deployment remain the boundary.

## Relationships

- Depends on: [Agent Strict Mode](/rules/agent-strict-mode.md)
- Depends on: [Lockdown Mode](/rules/lockdown-mode.md)
- Depends on: [Reveal Gate](/rules/reveal-gate.md)
- Uses: [Operation Broker](/components/operation-broker.md)
