---
type: Architecture Decision
title: Broker Secrets Instead of Injecting Agent Commands
description: Strict agent isolation uses fixed broker operations because output filtering cannot secure a secret already delivered to agent-controlled code.
---

## Decision

Preserve pull -- cmd and pull --inject as compatibility workflows, but refuse them in Agent Strict Mode. Production agent workflows use built-in fixed-destination HTTP operations and a constrained broker. Output scrubbing remains defense in depth only. Deployments must put the broker and vault in an OS security domain unavailable to the coding agent; AGENTS.md policy is guidance, not the boundary.

## Relationships

- Applies to: [Agent Strict Mode](/rules/agent-strict-mode.md)
- Uses: [Operation Broker](/components/operation-broker.md)
