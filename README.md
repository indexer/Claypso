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

### Export the vault

```bash
calypso export ~/backup/vault.enc        # raw encrypted blob
calypso export --base64 > vault.txt      # base64 for git notes / password manager
```

The exported file is **still encrypted** — you need the master passphrase to use it.

### Import on another machine

```bash
calypso import ~/backup/vault.enc        # from raw file
calypso import --base64 vault.txt        # from base64
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
calypso add <name> --path <path>     # register a project
calypso remove <name>                # unregister (does not delete .env)
calypso list                         # table of all projects
```

### Variables

```bash
calypso set <name> KEY=value...      # add or update
calypso get <name> [KEY]             # view (masked by default)
calypso get <name> --reveal          # view real values
calypso unset <name> KEY...          # remove variables
```

### Sync

```bash
calypso push <name>                  # import .env → vault (confirms changes)
calypso push <name> --force          # skip confirmation
calypso pull <name>                  # export vault → .env (real values)
calypso pull <name> --safe           # masked values (****) for LLM safety
calypso pull <name> --example        # empty values for .env.example
calypso pull <name> -- command...    # --wipe: write, run command, shred
```

### Analysis

```bash
calypso diff <A> <B>                 # compare two projects
calypso diff <A> <B> --reveal        # with real values
calypso gaps                         # find missing keys across projects
calypso dashboard --port <N>         # web overview
```

### Vault management

```bash
calypso init                         # manual vault creation (optional)
calypso keychain save                # store passphrase in OS keychain
calypso keychain forget              # remove from keychain
calypso keychain status              # check keychain status
calypso export <path>                # backup encrypted vault to file
calypso export --base64              # backup as base64 (stdout)
calypso import <path>                # restore vault from backup
calypso import --base64 <path>       # restore from base64
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
cmd/calypso/         CLI (Cobra), 17 commands
internal/
  crypto/            Argon2id + NaCl secretbox
  vault/             Encrypted vault CRUD + file locking
  project/           .env parse (multiline), serialize, I/O
  analysis/          Diff, gaps, key matrix
  dashboard/         Web UI (go:embed template)
  lockfile/          Advisory flock(2) locking
  keychain/          macOS Keychain / Linux libsecret
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
