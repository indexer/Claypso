---
type: Business Rule
title: Output Scrubbing
description: Child stdout/stderr in compatibility pull -- cmd runs is filtered; secret values become a name-free <concealed> marker.
---

## Rule

wipeAndRun wraps the child's stdout/stderr in a streaming scrubWriter (cmd/calypso/scrub.go) that replaces every occurrence of a vault value (length >= 4) with <concealed>, holding back up to maxLen-1 bytes across write boundaries so split values are still caught. On by default. --no-scrub is an unsafe reveal-class option requiring a human TTY and is blocked by Lockdown and Agent Strict Mode. pull --inject skips writing the real .env entirely and passes values only via the child process environment, but the whole arbitrary-command workflow is blocked by Agent Strict Mode.

## Relationships

- Applies to: [Pull Workflow](/workflows/pull-workflow.md)
