---
type: Business Rule
title: Mask By Default
description: Real secret values are never printed unless the user explicitly asks with --reveal.
---

## Rule

get and diff mask values (first 2 + last 2 chars, or fixed-width asterisks); --reveal is required to see real values; --hint shows edge characters only. The dashboard binds 127.0.0.1 and never shows values. This is the primary defense against secrets leaking into AI-agent transcripts, screenshots, and logs.

## Relationships

- Applies to: [Environment Variable](/concepts/environment-variable.md)
