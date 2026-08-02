---
type: Architecture Decision
title: Adopt OKF Project Knowledge
description: Agent-facing knowledge lives in OKF Markdown documents.
---

## Decision

Project knowledge is authored in .noli/concepts.yaml and rendered into knowledge/ by the Noli CLI. Coding agents retrieve it through noli retrieve instead of reading files directly.
