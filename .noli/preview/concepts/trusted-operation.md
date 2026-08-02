---
type: Domain Entity
title: Trusted Operation
description: Owner-configured, opaque HTTP operation that binds one vault secret to a fixed request without exposing the secret name or value to an agent.
---

## Definition

A Trusted Operation has a random opaque ID, an owner-facing label, a fixed project@env reference, HTTPS endpoint, HTTP method, secret key binding, destination header, optional non-secret prefix, expected status codes, and timeout. The definition lives only inside schema v3 of the encrypted vault. Agents may invoke an operation by opaque ID but cannot supply a URL, method, header, secret key, request body, or arbitrary command at run time.

## Relationships

- Depends on: [Project Environment](/concepts/project-environment.md)
- Enforced by: [Agent Strict Mode](/rules/agent-strict-mode.md)
