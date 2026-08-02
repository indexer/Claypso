---
type: Business Rule
title: Exposure Check
description: calypso exposure warns when a .env holds real vault values on disk longer than a threshold.
---

## Rule

A key is "hot" when its on-disk value equals the vault's real value and is at least 4 bytes; placeholders, empty, and rotated values never count. Age comes from the file's mtime. Exit is non-zero when any env has been hot for at least --max-age (default 30m), making it cron/CI usable like drift. --fix rewrites hot files with masked values, but refuses when the disk holds unsynced local edits unless --force is given. Plain pull prints a reminder nudge to stderr.

## Relationships

- Applies to: [Environment Variable](/concepts/environment-variable.md)
- Depends on: [LLM Safe Mode](/rules/llm-safe-mode.md)
