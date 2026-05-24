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

Store the master passphrase in your OS keychain:

```bash
calypso keychain save     # prompt once, store in keychain
calypso keychain status   # check if stored
calypso keychain forget   # remove from keychain
```

After `keychain save`, every `calypso` command retrieves the passphrase
automatically — no more typing.

| Platform | Backend | Install |
|----------|---------|---------|
| macOS | Keychain | Built-in |
| Linux | libsecret | `apt install libsecret-tools` |

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
