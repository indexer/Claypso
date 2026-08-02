---
type: Domain Entity
title: Project Environment
description: Named environment (dev/prod/staging) inside a project, each mapped to its own .env path.
---

## Definition

A project holds one or more environments addressed as name@env (bare name means the invisible "default" env when only one exists). Every variable-touching command (set, get, unset, push, pull, diff, remove, drift) accepts the name@env spec. env copy deep-copies all variables from one env to another.

## Relationships

- Depends on: [Vault](/concepts/vault.md)
