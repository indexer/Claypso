---
type: Workflow
title: Push Workflow
description: Import an on-disk .env into the vault with a diff summary and confirmation.
---

## Steps

push <spec> parses the .env, shows +added/~changed/-removed/=unchanged counts, asks "Overwrite vault values with .env? [y/N]" (skippable with --force), then saves. calypso drift detects when .env was edited outside calypso (non-zero exit on drift, usable as CI guard).

## Relationships

- Uses: [Vault Component](/components/vault-component.md)
