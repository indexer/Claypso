---
type: Application Component
title: Operation Broker
description: Hardened in-process executor and loopback HTTP capability service for Trusted Operations.
---

## Responsibility

internal/operation validates and executes fixed HTTP operations with redirects disabled, bounded timeouts, fixed destinations, no raw response output, and generic errors. cmd/calypso/commands_operation.go manages definitions outside strict mode and invokes them directly. cmd/calypso/commands_broker.go serves only opaque operation invocation endpoints on 127.0.0.1, authenticated by a random capability token written to an owner-selected connection file (0600 by default, or explicit 0640 for a dedicated shared group). Serving requires an explicit operation-ID allowlist; a connection capability cannot invoke other vault operations. The broker is intended to run under an OS identity that can read the vault while the coding agent can access only the constrained broker.

## Relationships

- Uses: [Vault Component](/components/vault-component.md)
- Uses: [Trusted Operation](/concepts/trusted-operation.md)
