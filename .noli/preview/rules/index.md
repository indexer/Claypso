# Business Rule

* [Agent Strict Mode](/rules/agent-strict-mode.md) - Vault-stored state that blocks every path exposing credential names or values to agent-controlled files, output, commands, or configuration.
* [Audit Log](/rules/audit-log.md) - Reveal-class and control operations append hash-chained events to <vault>.audit.log.
* [Exposure Check](/rules/exposure-check.md) - calypso exposure warns when a .env holds real vault values on disk longer than a threshold.
* [LLM Safe Mode](/rules/llm-safe-mode.md) - .env on disk can be masked so AI coding agents see key names but never real values.
* [Lockdown Mode](/rules/lockdown-mode.md) - Vault-stored flag that disables reveal-class commands regardless of TTY or env vars.
* [Mask By Default](/rules/mask-by-default.md) - Real secret values are never printed unless the user explicitly asks with --reveal.
* [Output Scrubbing](/rules/output-scrubbing.md) - Child stdout/stderr in compatibility pull -- cmd runs is filtered; secret values become a name-free <concealed> marker.
* [Passphrase Handling](/rules/passphrase-handling.md) - Master passphrase is the single key; read without echo, zeroed after use, never recoverable.
* [Reveal Gate](/rules/reveal-gate.md) - Reveal-class commands require an interactive terminal or explicit CALYPSO_UNATTENDED=1.
* [Vault Encryption Scheme](/rules/vault-encryption-scheme.md) - Argon2id (64 MB, 3 iterations, 4 threads) key derivation plus NaCl secretbox authenticated encryption.
