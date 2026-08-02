---
type: Domain Entity
title: Environment Variable
description: KEY=value secret stored in the vault; masked by default everywhere it is displayed.
---

## Definition

Parsed from .env files (export prefix, quotes, multiline, escapes, comments handled). Values are held in memguard enclaves in memory (project.Secret) and displayed masked unless --reveal is passed.

## Relationships

- Depends on: [Project Environment](/concepts/project-environment.md)
- Enforced by: [Mask By Default](/rules/mask-by-default.md)
