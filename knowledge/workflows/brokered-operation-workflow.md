---
type: Workflow
title: Brokered Operation Workflow
description: An agent invokes an opaque capability while Calypso alone resolves and uses the bound credential.
---

## Steps

1) The owner configures and validates a Trusted Operation before enabling strict mode. 2) The owner enables Agent Strict Mode and starts the broker outside the agent security domain. 3) The agent submits only an opaque operation ID with the broker capability token. 4) Calypso resolves the encrypted definition and secret, performs the fixed request with redirects disabled, drains and discards a bounded response body, and returns a schema-limited result. 5) The audit log records the opaque operation ID and outcome, never its credential binding.

## Relationships

- Follows: [Agent Strict Mode](/rules/agent-strict-mode.md)
- Uses: [Operation Broker](/components/operation-broker.md)
