# calypso

A local, encrypted manager for your projects' `.env` files. One vault holds
every project's environment. **Nothing ever leaves your machine.**

## Install

Requires **Go 1.22+**.

```bash
git clone https://github.com/yemon/calypso.git
cd calypso
go install ./cmd/calypso

# Add to your shell config (.zshrc / .bashrc):
export PATH="$HOME/go/bin:$PATH"
```

Verify:

```bash
calypso version
# calypso 1.0.0
#   commit: abc1234
#   go:     go1.22.1
```

## Getting Started (step by step)

### 1. Register your first project

No `init` needed — the vault is created on first use.

```bash
calypso add myapp --path ~/projects/myapp/.env
```

You'll be prompted to create a master passphrase. This is the **only key**
you'll ever need — guard it carefully, it cannot be recovered.

### 2. Import your existing `.env`

If you already have a `.env` file with secrets:

```bash
calypso push myapp
```

This shows a diff summary before overwriting:

```
Changes in "myapp":
  +3 added  ~0 changed  -0 removed  =0 unchanged

Overwrite vault values with .env? [y/N]: y
```

Skip the prompt with `--force`:

```bash
calypso push myapp --force
```

### 3. Add more variables

```bash
calypso set myapp OPENAI_API_KEY=sk-abc123 GEMINI_API_KEY=ai-xyz789
```

### 4. View your vault

```bash
calypso list                          # overview of all projects
calypso get myapp                     # masked values (safe to share)
calypso get myapp --reveal            # real values (be careful)
```

### 5. Write values to `.env`

```bash
calypso pull myapp                    # write real values
```

## LLM Safety (core workflow)

Prevent AI coding assistants from reading your secrets:

### Before sharing code with an LLM

```bash
calypso pull myapp --safe
```

Your `.env` becomes:

```
# Managed by calypso. Safe mode — values are masked.
DB_HOST=****
API_KEY=****
DATABASE_URL=****
```

The LLM can see **what keys exist** but never the actual values.

### After the LLM session

```bash
calypso pull myapp                    # restore real .env
```

### One-shot: run a command safely

Write real `.env`, execute a command, automatically shred when done:

```bash
calypso pull myapp -- npm test --verbose
# 1. Writes real .env
# 2. Runs `npm test --verbose`
# 3. Overwrites .env with **** (always, even on failure)
```

## Multiple Environments (staging / dev / production)

Each project can have multiple environments, each with its own `.env` file.
Use `name@env` to target a specific one, or bare `name` when there's only one.

### Setup

```bash
# Create a project with a dev environment
calypso add myapp --env dev --path .env.dev

# Add a production environment to the same project
calypso add myapp@prod --path .env.production

# Add staging by cloning from dev
calypso env copy myapp@dev staging --path .env.staging
```

### Day-to-day

```bash
calypso set myapp@dev API_KEY=sk-test
calypso set myapp@prod API_KEY=sk-live STRIPE_KEY=sk_live_abc
calypso push myapp@staging --force

# Pull per environment
calypso pull myapp@dev --safe       # mask before LLM sessions
calypso pull myapp@staging          # write real values
```

### View environments

```bash
calypso env list myapp
```

```
ENV      VARS  UPDATED               PATH
dev      3     2026-05-23T10:00:00Z  .env.dev
prod     5     2026-05-23T10:01:00Z  .env.production
staging  3     2026-05-23T10:02:00Z  .env.staging
```

### Compare environments of the same project

```bash
calypso diff myapp@dev myapp@prod --reveal
```

```
KEY        myapp@dev    myapp@prod    STATUS
API_KEY    sk-test      sk-live       differs
STRIPE_KEY —            sk_live_abc   only in myapp@prod
```

### How it works

- Without `@env`, a project has one environment named `"default"` (invisible).
- Adding a second env enables the `name@env` syntax — bare `name` then returns
  a clear error listing available envs.
- `env copy` deep-copies all variables from one env to another.
- Every command that takes a project name supports `name@env`: `set`, `get`,
  `unset`, `push`, `pull`, `diff`, `remove`.

## Keychain (no more typing passphrases)

The OS keychain stores **the master passphrase** (not the env values)
so calypso can unlock the vault transparently on every command. After a
one-time setup, you stop typing the passphrase entirely.

### One-time setup

```bash
calypso keychain save
# Master passphrase: ************
# Passphrase stored in keychain.
```

That's it. From then on:

```bash
calypso get myapp@dev API_KEY     # no prompt
calypso pull myapp@prod            # no prompt
calypso set myapp@dev FOO=bar      # no prompt
```

Under the hood, every command's `openVault` tries the keychain first.
If it returns a working passphrase, the vault opens silently. If the
stored passphrase is wrong (you rotated it manually), calypso forgets
the keychain entry automatically and falls back to prompting.

### What's stored where

| Thing                  | Where                           | Protected by                 |
|------------------------|---------------------------------|------------------------------|
| Master passphrase      | OS keychain                     | OS-level user authentication |
| Env values             | `~/.calypso/vault.enc`          | Argon2id + NaCl secretbox    |
| Auto-backups           | `~/.calypso/vault.enc.backups/` | Same crypto as the vault     |

The vault file itself is **always** encrypted on disk. The keychain
just removes the typing step at unlock time — it does not lower the
encryption strength of your data at rest.

### Platform backends

| Platform | Backend                  | Install                                          |
|----------|--------------------------|--------------------------------------------------|
| macOS    | Keychain (`security`)    | Built-in                                         |
| Linux    | libsecret (`secret-tool`)| `apt install libsecret-tools` (or distro equiv.) |
| Windows  | not yet supported        | use `ENVHUB_PASSPHRASE` env var instead          |

Check what calypso sees:

```bash
calypso keychain status
# Keychain backend: available, passphrase is stored
```

If you see "not available" on Linux, install `libsecret-tools`.

### Removing it

```bash
calypso keychain forget
# Passphrase removed from keychain.
```

Calypso goes back to prompting on every command. Use this before
walking away from a shared machine, or before handing off ownership of
your workstation.

### Rotating the passphrase

Calypso doesn't yet ship a built-in re-key command. The manual flow:

```bash
calypso export ~/old.enc                  # backup current vault
calypso keychain forget                   # drop the cached passphrase
# move ~/.calypso/vault.enc aside, then `calypso init` with the new passphrase
# import projects from your backup with `calypso import --merge` if needed
calypso keychain save                     # cache the new passphrase
```

A first-class `calypso vault rekey` is on the roadmap.

### For AI coding agents (Cursor, Claude Code, Codex, Cline, …)

If you have an AI agent driving a terminal — Cursor's terminal, Claude
Code, Codex CLI, Cline, Aider, or anything else that shells out — a
passphrase prompt **will block the agent**. The agent can't see the
hidden password field, can't type into it, and the command hangs
forever (or fails immediately with `inappropriate ioctl for device`
when there's no TTY). The agent is then stuck and your turn dies.

The keychain fixes this. Do this *once* in your own terminal (not in
the agent's):

```bash
calypso keychain save
# Master passphrase: ************
# Passphrase stored in keychain.
```

From then on, the agent can run calypso freely:

```bash
# Agent terminal — these all succeed without any prompt
calypso list
calypso get myapp@dev DB_HOST              # masked: lo***st
calypso pull myapp@dev                     # writes real values to .env
calypso pull myapp@dev -- npm test         # runs your tests with real values
calypso drift myapp@dev                    # alerts if .env was hand-edited
```

#### Remote/cloud dev environments

If the agent is running in a place where the OS keychain isn't
available — Codespaces, Gitpod, Coder, a remote ssh box, a cloud agent
that spins up containers — `keychain save` will fail or be useless.
Use `ENVHUB_PASSPHRASE` instead. Set it as a workspace secret (NOT in
a tracked file):

```bash
# Set ONCE in your workspace's env-config UI:
export ENVHUB_PASSPHRASE="your-master-passphrase"

# Then the agent runs the same commands as above, prompt-free:
calypso pull myapp@dev -- npm test
```

GitHub Codespaces: add it under *Settings → Codespaces → Secrets*.
Gitpod: under *Variables*. Most cloud agents have an equivalent
"environment" or "secrets" panel — they get injected into every shell
the agent opens, so no prompt is ever needed.

#### What can the agent see after this setup?

Once the agent can unlock the vault (via keychain or
`ENVHUB_PASSPHRASE`), it has **the same access you have**:

| Command                          | What the agent sees                                |
|----------------------------------|----------------------------------------------------|
| `calypso get myapp KEY`          | Masked value (`sk***23`)                           |
| `calypso get myapp KEY --reveal` | **Real value** (`sk-test-abc123`)                  |
| `calypso pull myapp`             | Writes **real values** to `.env` on disk           |
| `calypso pull myapp --safe`      | Writes `****` placeholders                         |
| `calypso pull myapp -- cmd…`     | Real values during `cmd`, wiped to `****` after    |

If you're happy with the agent seeing real values (it needs them to
run your code anyway), this is fine. If you'd rather it didn't print
secrets to chat where you might screenshot or paste them, lock it
down at the tool layer:

- **Cursor / Claude Code / Cline**: use the permission allowlist.
  Allow `calypso list`, `calypso get` (without `--reveal`), `calypso
  pull -- cmd…`, `calypso drift`. Disallow `calypso get --reveal`,
  `calypso pull` (without a trailing `-- cmd`), `calypso export`.
- **Prompt convention**: tell the agent "use `calypso pull X --safe`
  whenever you need to show me .env structure" and "use `calypso pull
  X -- cmd…` to run code that needs real values — never plain `pull`".
- **Inject, don't write**: `calypso pull myapp -- npm test` injects
  real values into the subprocess, then immediately overwrites the
  `.env` with `****` placeholders. The agent's next read of `.env`
  sees masked values, not real ones.

The tool-permission allowlist in your agent is the enforcement point —
calypso happily prints real values when asked, so the discipline of
*not* asking has to live in the agent's configuration.

### How it interacts with `ENVHUB_PASSPHRASE`

If `ENVHUB_PASSPHRASE` is set, calypso uses it and **skips the keychain
entirely** — handy for CI scripts that shouldn't touch the user's
keychain, and for `keychain save` itself (it stores whatever
`ENVHUB_PASSPHRASE` provides without re-prompting).

### Security tradeoffs (read before turning it on)

The keychain is convenient because it removes a friction step. It also
moves the security boundary, so know what you're trading.

**What the keychain gives you**
- The vault file stays encrypted with the same Argon2id + secretbox
  scheme. Stealing `vault.enc` alone is still useless without the
  passphrase.
- The passphrase lives in a per-user OS-managed credential store, not
  in a config file, env var, or your shell history.
- macOS Keychain and libsecret both gate access with the OS user
  session. A locked screen means a locked passphrase.

**What it gives up**
- *Any* process running as your user can read the passphrase. On
  Linux: `secret-tool lookup app calypso`. On macOS:
  `security find-generic-password -a calypso -w`. That includes
  shell scripts you ran, malware that compromised your account, and
  the LLM coding agent in your editor. The passphrase is no longer
  the security boundary — your user account is.
- Backups taken with `calypso vault backups` or `calypso export` will
  decrypt with the keychain-cached passphrase. Anyone with both your
  user account and your vault file gets your secrets.

**When to use it**
- ✅ Your personal laptop, where you trust everything running as you.
- ✅ A workstation where you accept that compromise-of-user equals
  compromise-of-secrets (which is usually true regardless).
- ❌ Shared machines (multiple humans logging in as the same user).
- ❌ CI runners and remote agents — use `ENVHUB_PASSPHRASE` for the
  duration of the job instead.
- ❌ Machines where you run untrusted code as your user (e.g. a
  freshly cloned repo with build scripts you haven't audited).

## Backup & Transfer

### Auto-backup (built in, on by default)

Every successful save also writes a timestamped copy to
`<vault>.backups/<UTC-ns-timestamp>.enc`. No setup needed — the backups
appear from your first `add` onwards.

```bash
calypso vault backups list
```

```
NAME                                AGE
20260524T091500.000000000Z.enc      2h ago
20260524T091723.451200000Z.enc      2h ago
20260524T112048.998700000Z.enc      14m ago
```

Roll back to any of them:

```bash
calypso vault backups restore 20260524T091500.000000000Z.enc
# → restores that backup, snapshots the current vault to
#   <vault>.pre-restore.bak so the restore is itself undoable
```

Tune retention (default 10; `0` disables auto-backup entirely):

```bash
export CALYPSO_BACKUP_RETAIN=30      # keep last 30 saves
export CALYPSO_BACKUP_RETAIN=0       # disable
```

Backups are byte-identical to the vault file (same encryption, same
passphrase). Restoring is a file copy, not a re-encrypt.

### Export the whole vault

```bash
calypso export ~/backup/vault.enc        # raw encrypted blob
calypso export --base64 > vault.txt      # base64 for git notes / password managers
```

The exported file is **still encrypted** — you need the master passphrase to use it.

### Export one project (or one env)

For sharing a single project's setup with a teammate, or a targeted
backup of just the env you care about:

```bash
calypso vault export-project myapp /share/myapp.cbk          # all envs
calypso vault export-project myapp@prod /share/myapp-prod.cbk # just prod
```

The output blob uses the same encryption as a full vault export — the
recipient needs the master passphrase to import.

### Import on another machine

```bash
calypso import ~/backup/vault.enc                # full-vault restore (default)
calypso import --base64 vault.txt                # same, from base64

calypso import /share/myapp.cbk --merge          # partial export → merge into current vault
calypso import /share/myapp.cbk --merge --force  # also replace an existing project of the same name
```

Default `import` blind-copies the encrypted blob over the current vault
file — no passphrase needed at import time. The `--merge` flag opts into
the partial-export path: decrypts the blob (prompts for the master
passphrase), confirms it's a `vault export-project` output, and merges
just the project into your existing vault.

## Vault health

### Verify

`calypso vault verify` is a read-only health check — useful in cron or
as a pre-deploy guard.

```bash
calypso vault verify
```

```
Findings:
  [WARNING] myapp@dev: Path "relative/.env" is relative [fixable]
  [WARNING] myapp: UpdatedAt is empty [fixable]
```

Exit code is non-zero if any **error**-severity issue is found
(warnings alone don't fail). Add `--fix` to apply the trivial repairs
(sync names, fill timestamps, normalise relative paths) and save:

```bash
calypso vault verify --fix
```

```
Findings:
  [WARNING] myapp@dev: Path "relative/.env" is relative [fixable]

Applied:
  - myapp@dev: normalised Path (was "relative/.env")
  - myapp: set UpdatedAt

No issues remain.
```

The pre-fix vault is preserved by the regular auto-backup.

### Detect `.env` drift

When someone (or some other tool) edits `.env` directly without going
through calypso, the vault and disk diverge. `calypso drift` shows it.

```bash
calypso drift              # scan every env
calypso drift myapp@prod   # one env
```

```
myapp@prod  (/srv/myapp/.env.production)
  +1 added  ~1 changed  -0 removed  =4 unchanged

myapp@staging  (/srv/myapp/.env.staging)
  +0 added  ~0 changed  -0 removed  =5 unchanged
```

Use `--details` to see which keys changed, and `--reveal` to see the
actual values:

```bash
calypso drift myapp@prod --details --reveal
```

```
myapp@prod  (/srv/myapp/.env.production)
  +1 added  ~1 changed  -0 removed  =4 unchanged
  KEY        VAULT             DISK             KIND
  API_KEY    sk-vault-value    sk-edited-value  changed
  HOTFIX     —                 enabled          only on disk
```

Exit code is non-zero on any drift, so this works as a CI guard:

```bash
calypso drift || { echo ".env drifted from vault — investigate"; exit 1; }
```

## Shell Completion

```bash
# bash
calypso completion bash > ~/.bash_completion

# zsh
calypso completion zsh > ~/.zfunc/_calypso

# fish
calypso completion fish > ~/.config/fish/completions/calypso.fish
```

## CI / Scripts

Set the `ENVHUB_PASSPHRASE` environment variable for non-interactive use:

```bash
export ENVHUB_PASSPHRASE="my-master-passphrase"
calypso set myapp CI_TOKEN=ghp_123
calypso pull myapp
```

## `.env.example` generation

Create a template for documentation or git:

```bash
calypso pull myapp --example
# .env becomes:
#   DB_HOST=
#   API_KEY=
#   DATABASE_URL=
```

Commit this as `.env.example` so contributors know what keys are needed.

## Multi-Project Management

### Compare two projects

```bash
calypso diff staging production --reveal
```

Output:

```
KEY        staging           production        STATUS
API_KEY    sk-staging-key    sk-prod-key       differs
DB_HOST    db.staging.com    db.prod.com       differs
DEBUG      1                 —                 only in staging
LOG_LEVEL  —                 warn              only in production
```

### Find missing keys across all projects

```bash
calypso gaps
```

Flags projects that are missing a key their siblings define — common source of
"works on my machine" bugs.

### Dashboard

```bash
calypso dashboard --port 7777
# → http://127.0.0.1:7777
```

Read-only web overview: project list, gap matrix, key usage table.
Values are **never shown**.

## Complete Command Reference

### Project management

```bash
calypso add <name> --path <path>           # register a project
calypso add <name> --env <env> --path <p>  # register with named env
calypso add <name@env> --path <p>          # add an env to existing project
calypso remove <name[@env]>                # remove project or single env
calypso list                               # table of all projects + envs
```

### Environment management

```bash
calypso env list <project>                 # list environments in a project
calypso env copy <src@env> <dst> --path <p> # clone an env's vars into a new one
```

### Variables

```bash
calypso set <name[@env]> KEY=value...      # add or update
calypso get <name[@env]> [KEY]             # view (masked by default)
calypso get <name[@env]> --reveal          # view real values
calypso unset <name[@env]> KEY...          # remove variables
```

### Sync

```bash
calypso push <name[@env]>                  # import .env → vault (confirms changes)
calypso push <name[@env]> --force          # skip confirmation
calypso pull <name[@env]>                  # export vault → .env (real values)
calypso pull <name[@env]> --safe           # masked values (****) for LLM safety
calypso pull <name[@env]> --example        # empty values for .env.example
calypso pull <name[@env]> -- command...    # --wipe: write, run command, shred
```

### Analysis

```bash
calypso diff <A[@env]> <B[@env]>             # compare two projects or envs
calypso diff <A[@env]> <B[@env]> --reveal     # with real values
calypso gaps                                 # find missing keys across all
calypso drift [project[@env]]                # compare vault vs on-disk .env
calypso drift [project[@env]] --details      # per-key listing
calypso drift [project[@env]] --reveal       # show real values in --details
calypso dashboard --port <N>                 # web overview
```

### Vault management

```bash
calypso init                                 # manual vault creation (optional)

calypso keychain save                        # store passphrase in OS keychain
calypso keychain forget                      # remove from keychain
calypso keychain status                      # check keychain status

calypso export <path>                        # full-vault backup to file
calypso export --base64                      # full-vault backup to stdout (base64)
calypso import <path>                        # restore full vault (no passphrase needed)
calypso import --base64 <path>               # restore from base64
calypso import <path> --merge [--force]      # merge a partial export into current vault

calypso vault verify                         # sanity-check (read-only)
calypso vault verify --fix                   # apply trivial repairs and save
calypso vault downgrade                      # rewrite as legacy v1 (single-env only)

calypso vault backups list                   # show auto-backups
calypso vault backups restore <name>         # roll back to a backup
calypso vault export-project <spec> <path>   # export one project or env
```

### Other

```bash
calypso completion <shell>           # generate shell completion
calypso version                      # print version info
```

## Security

### Encryption

- **Argon2id** (64 MB, 3 iter, 4 threads) → GPU/ASIC resistant key derivation
- **NaCl secretbox** (XSalsa20-Poly1305) → authenticated encryption
- Fresh **salt + nonce** per save → no key reuse

### File safety

- Vault: `0600` perms, `.env` files: `0600` perms
- Atomic writes (`.tmp` → `rename`) → never partial, crash-safe
- `flock(2)` advisory locking → no two processes can write simultaneously

### Passphrase handling

- Read **without echo** (terminal raw mode)
- **SIGINT safe** — Ctrl+C during entry restores terminal state
- **Zeroed in memory** after every operation
- 3 **retry attempts** with increasing delay (500ms → 1s → 1.5s)
- Supports `ENVHUB_PASSPHRASE` for CI

### Defaults

- `get` and `diff` **mask** values (first 2 + last 2 chars)
- `--reveal` required to see real values
- Dashboard binds `127.0.0.1`, shows **no values**

## `.env` Parsing

- `KEY=value` — basic assignment
- `export KEY=value` — export prefix stripped
- `"double"` / `'single'` quoted values — quotes removed
- Multiline quoted values
- Escape sequences: `\n`, `\t`, `\"`, `\\`
- `# comments` and blank lines ignored
- `=` inside values handled correctly

## Architecture

```
cmd/calypso/           CLI (Cobra), 20+ commands
internal/
  crypto/              Argon2id + NaCl secretbox
  vault/               Encrypted vault + envs + file locking
  project/             .env parse (multiline), types, serialize
  analysis/            Diff, gaps, key matrix
  dashboard/           Web UI (go:embed template)
  lockfile/            Advisory flock(2) locking
  keychain/            macOS Keychain / Linux libsecret
```

## Development

```bash
make build          # build binary with version info
make test           # run unit tests
make test-int       # run integration tests (CLI lifecycle)
make lint           # run golangci-lint
make vet            # run go vet
make all            # lint → vet → test → build
make install        # install to $GOPATH/bin
make clean          # remove binary + test cache
```
