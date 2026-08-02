package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// agentCmd integrates calypso with AI coding agents. `agent init <tool>`
// writes tool-level deny rules around the in-binary agent-strict boundary and
// directs credential-bearing work through broker capabilities.
func agentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Integrate calypso with AI coding agents",
	}
	cmd.AddCommand(agentInitCmd())
	return cmd
}

func agentInitCmd() *cobra.Command {
	var global, dryRun bool
	var dir string
	cmd := &cobra.Command{
		Use:       "init <claude-code|codex|opencode>",
		Short:     "Write agent-tool rules that keep calypso secrets out of transcripts",
		ValidArgs: []string{"claude-code", "codex", "opencode"},
		Long: `agent init writes secret-safety rules into an AI coding agent's own
configuration:

  claude-code   merge deny/allow Bash rules into .claude/settings.json
                (or ~/.claude/settings.json with --global)
  opencode      merge deny globs into opencode.json permission.bash
                (or ~/.config/opencode/opencode.json with --global)
  codex         Codex has no per-command deny, so an advisory policy
                section is written into AGENTS.md and a
                shell_environment_policy snippet for ~/.codex/config.toml
                is printed for you to add manually

Existing settings are preserved: rules are merged and deduped, unrelated
keys are never touched. Use --dry-run to preview.

These rules are defense in depth. The hard stops live in agent strict mode;
production setups run the broker under a separate OS identity so the agent can
access only a constrained connection capability.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var res agentInitResult
			var err error
			switch args[0] {
			case "claude-code":
				res, err = initClaudeCode(dir, global, dryRun)
			case "opencode":
				res, err = initOpenCode(dir, global, dryRun)
			case "codex":
				res, err = initCodex(dir, dryRun)
			default:
				return fmt.Errorf("unknown tool %q (supported: claude-code, codex, opencode)", args[0])
			}
			if err != nil {
				return err
			}
			res.print(dryRun)
			return nil
		},
	}
	cmd.Flags().BoolVar(&global, "global", false, "write to the user-level config instead of the project")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be written without touching any file")
	cmd.Flags().StringVar(&dir, "dir", ".", "project directory to write into")
	return cmd
}

type agentInitResult struct {
	path    string   // file written (or that would be, with --dry-run)
	added   []string // rules/sections newly added
	skipped []string // already present
	notes   []string // manual follow-ups
	preview string   // full file content, shown on --dry-run
}

func (r agentInitResult) print(dryRun bool) {
	verb := "Wrote"
	if dryRun {
		verb = "Would write"
	}
	fmt.Printf("%s %s\n", verb, r.path)
	for _, a := range r.added {
		fmt.Printf("  + %s\n", a)
	}
	for _, s := range r.skipped {
		fmt.Printf("  = %s (already present)\n", s)
	}
	if dryRun && r.preview != "" {
		fmt.Printf("\n%s\n", r.preview)
	}
	for _, n := range r.notes {
		fmt.Printf("\n%s\n", n)
	}
	fmt.Println("\nReminder: agent strict mode and the broker are the boundary; these tool rules are defense in depth.")
}

// --- claude-code ------------------------------------------------------

// Claude Code Bash rules are prefix matches (optionally ending in :*), so a
// mid-command flags cannot always be pattern-denied. Agent strict mode remains
// the in-binary enforcement while these prefix rules reduce accidental probes.
var claudeCodeDeny = []string{
	"Bash(CALYPSO_UNATTENDED=*)",
	"Bash(env CALYPSO_UNATTENDED=*)",
	"Bash(export CALYPSO_UNATTENDED=*)",
	"Bash(calypso export)",
	"Bash(calypso export:*)",
	"Bash(calypso import:*)",
	"Bash(calypso pull:*)",
	"Bash(calypso push:*)",
	"Bash(calypso get:*)",
	"Bash(calypso set:*)",
	"Bash(calypso unset:*)",
	"Bash(calypso add:*)",
	"Bash(calypso remove:*)",
	"Bash(calypso diff:*)",
	"Bash(calypso gaps)",
	"Bash(calypso gaps:*)",
	"Bash(calypso drift)",
	"Bash(calypso drift:*)",
	"Bash(calypso exposure)",
	"Bash(calypso exposure:*)",
	"Bash(calypso dashboard)",
	"Bash(calypso dashboard:*)",
	"Bash(calypso sync:*)",
	"Bash(calypso vault:*)",
	"Bash(calypso keychain:*)",
	"Bash(calypso env copy:*)",
	"Bash(calypso operation add-http:*)",
	"Bash(calypso operation remove:*)",
	"Bash(calypso lockdown off)",
	"Bash(calypso lockdown off:*)",
	"Bash(calypso strict off)",
	"Bash(calypso strict off:*)",
	"Bash(secret-tool lookup:*)",
	"Bash(security find-generic-password:*)",
}

var claudeCodeAllow = []string{
	"Bash(calypso list)",
	"Bash(calypso env list:*)",
	"Bash(calypso strict status)",
	"Bash(calypso lockdown status)",
	"Bash(calypso operation list)",
	"Bash(calypso broker invoke:*)",
	"Bash(calypso audit list:*)",
	"Bash(calypso audit verify)",
}

func initClaudeCode(dir string, global, dryRun bool) (agentInitResult, error) {
	path := filepath.Join(dir, ".claude", "settings.json")
	if global {
		home, err := os.UserHomeDir()
		if err != nil {
			return agentInitResult{}, err
		}
		path = filepath.Join(home, ".claude", "settings.json")
	}
	res := agentInitResult{path: path}

	cfg, err := readJSONMap(path)
	if err != nil {
		return res, err
	}
	perms, err := subMap(cfg, "permissions")
	if err != nil {
		return res, fmt.Errorf("%s: %w", path, err)
	}
	for listKey, rules := range map[string][]string{"deny": claudeCodeDeny, "allow": claudeCodeAllow} {
		merged, added, skipped, err := mergeStringList(perms[listKey], rules)
		if err != nil {
			return res, fmt.Errorf("%s: permissions.%s: %w", path, listKey, err)
		}
		perms[listKey] = merged
		for _, a := range added {
			res.added = append(res.added, fmt.Sprintf("%s: %s", listKey, a))
		}
		for _, s := range skipped {
			res.skipped = append(res.skipped, fmt.Sprintf("%s: %s", listKey, s))
		}
	}
	cfg["permissions"] = perms
	return res, writeJSONMap(path, cfg, &res, dryRun)
}

// --- opencode ---------------------------------------------------------

// opencode permission.bash supports glob patterns with an explicit "deny"
// verdict, so — unlike Claude Code — a mid-command --reveal CAN be denied.
var opencodeDeny = map[string]string{
	"calypso*--reveal*":               "deny",
	"calypso*--no-scrub*":             "deny",
	"calypso export*":                 "deny",
	"calypso import*":                 "deny",
	"calypso pull*":                   "deny",
	"calypso push*":                   "deny",
	"calypso get*":                    "deny",
	"calypso set*":                    "deny",
	"calypso unset*":                  "deny",
	"calypso add*":                    "deny",
	"calypso remove*":                 "deny",
	"calypso diff*":                   "deny",
	"calypso gaps*":                   "deny",
	"calypso drift*":                  "deny",
	"calypso exposure*":               "deny",
	"calypso dashboard*":              "deny",
	"calypso sync*":                   "deny",
	"calypso vault*":                  "deny",
	"calypso keychain*":               "deny",
	"calypso env copy*":               "deny",
	"calypso operation add-http*":     "deny",
	"calypso operation remove*":       "deny",
	"calypso lockdown off*":           "deny",
	"calypso strict off*":             "deny",
	"CALYPSO_UNATTENDED=*":            "deny",
	"secret-tool lookup*":             "deny",
	"security find-generic-password*": "deny",
}

func initOpenCode(dir string, global, dryRun bool) (agentInitResult, error) {
	path := filepath.Join(dir, "opencode.json")
	if global {
		home, err := os.UserHomeDir()
		if err != nil {
			return agentInitResult{}, err
		}
		path = filepath.Join(home, ".config", "opencode", "opencode.json")
	}
	res := agentInitResult{path: path}

	cfg, err := readJSONMap(path)
	if err != nil {
		return res, err
	}
	if _, ok := cfg["$schema"]; !ok && len(cfg) == 0 {
		cfg["$schema"] = "https://opencode.ai/config.json"
	}
	perm, err := subMap(cfg, "permission")
	if err != nil {
		return res, fmt.Errorf("%s: %w", path, err)
	}

	// permission.bash may be a bare verdict string ("allow"/"ask"); lift it
	// into a pattern map with "*" carrying the old default before adding rules.
	var bash map[string]any
	switch cur := perm["bash"].(type) {
	case nil:
		bash = map[string]any{}
	case string:
		bash = map[string]any{"*": cur}
		res.notes = append(res.notes, fmt.Sprintf("permission.bash was %q; kept it as the \"*\" default.", cur))
	case map[string]any:
		bash = cur
	default:
		return res, fmt.Errorf("%s: permission.bash has unexpected type %T", path, cur)
	}
	for pattern, verdict := range opencodeDeny {
		if cur, ok := bash[pattern]; ok {
			if cur == verdict {
				res.skipped = append(res.skipped, pattern)
			} else {
				bash[pattern] = verdict
				res.added = append(res.added, fmt.Sprintf("%s: %v → %s (overridden)", pattern, cur, verdict))
			}
			continue
		}
		bash[pattern] = verdict
		res.added = append(res.added, fmt.Sprintf("%s: %s", pattern, verdict))
	}
	perm["bash"] = bash
	cfg["permission"] = perm
	return res, writeJSONMap(path, cfg, &res, dryRun)
}

// --- codex ------------------------------------------------------------

const (
	codexPolicyStart = "<!-- calypso agent policy: start -->"
	codexPolicyEnd   = "<!-- calypso agent policy: end -->"
)

const codexPolicySection = codexPolicyStart + `
## Secrets policy (managed by calypso)

- Treat the vault as agent-strict. Never run owner commands: ` + "`get`" + `,
  ` + "`set`" + `, ` + "`unset`" + `, ` + "`push`" + `, any form of ` + "`pull`" + `,
  ` + "`diff`" + `, ` + "`gaps`" + `, ` + "`drift`" + `, ` + "`exposure`" + `,
  ` + "`sync`" + `, imports/exports, vault/keychain mutation, operation
  mutation, ` + "`lockdown off`" + `, or ` + "`strict off`" + `.
- Never set or use the ` + "`CALYPSO_UNATTENDED`" + ` environment variable.
- Never read the OS keychain (` + "`secret-tool`" + `, ` + "`security find-generic-password`" + `).
- Never inspect registered .env structure; strict mode intentionally hides
  both credential names and values.
- Invoke an approved capability only with
  ` + "`calypso broker invoke <operation-id> --connection-file <path>`" + `.
- Safe metadata commands are ` + "`calypso list`" + `,
  ` + "`calypso env list`" + `, ` + "`calypso operation list`" + `,
  ` + "`calypso strict status`" + `, ` + "`calypso lockdown status`" + `,
  and ` + "`calypso audit verify`" + `.
` + codexPolicyEnd + "\n"

const codexTomlSnippet = `Codex has no per-command deny rules, so add this to ~/.codex/config.toml
yourself to keep calypso's env vars out of every shell Codex spawns:

  [shell_environment_policy]
  exclude = ["CALYPSO_PASSPHRASE", "ENVHUB_PASSPHRASE", "CALYPSO_NEW_PASSPHRASE", "CALYPSO_UNATTENDED"]`

func initCodex(dir string, dryRun bool) (agentInitResult, error) {
	path := filepath.Join(dir, "AGENTS.md")
	res := agentInitResult{path: path, notes: []string{codexTomlSnippet}}

	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return res, err
	}
	content := string(existing)

	updated, changed, err := upsertMarkedSection(content, codexPolicyStart, codexPolicyEnd, codexPolicySection)
	if err != nil {
		return res, fmt.Errorf("%s: %w", path, err)
	}
	if !changed {
		res.skipped = append(res.skipped, "secrets policy section")
	} else {
		res.added = append(res.added, "secrets policy section")
	}
	if dryRun {
		res.preview = updated
		return res, nil
	}
	if !changed {
		return res, nil
	}
	return res, os.WriteFile(path, []byte(updated), 0o644) //nolint:gosec // docs file, intentionally world-readable
}

// upsertMarkedSection replaces the start..end marked block in content with
// section, or appends it when absent. changed is false when the block is
// already identical.
func upsertMarkedSection(content, start, end, section string) (string, bool, error) {
	si := strings.Index(content, start)
	if si < 0 {
		out := content
		if out != "" && !strings.HasSuffix(out, "\n") {
			out += "\n"
		}
		if out != "" {
			out += "\n"
		}
		return out + section, true, nil
	}
	rel := strings.Index(content[si:], end)
	if rel < 0 {
		return "", false, fmt.Errorf("found %q without matching %q", start, end)
	}
	stop := si + rel + len(end)
	if stop < len(content) && content[stop] == '\n' {
		stop++
	}
	out := content[:si] + section + content[stop:]
	return out, out != content, nil
}

// --- shared helpers ---------------------------------------------------

// readJSONMap loads a JSON object from path; a missing file yields an empty
// map. Anything unparseable is refused rather than guessed at.
func readJSONMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s exists but is not valid JSON (%v); fix or remove it first", path, err)
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

// subMap returns cfg[key] as an object, creating it when absent.
func subMap(cfg map[string]any, key string) (map[string]any, error) {
	switch cur := cfg[key].(type) {
	case nil:
		return map[string]any{}, nil
	case map[string]any:
		return cur, nil
	default:
		return nil, fmt.Errorf("%q has unexpected type %T", key, cur)
	}
}

// mergeStringList appends the missing rules to a JSON string array, keeping
// existing entries and order.
func mergeStringList(existing any, rules []string) ([]any, []string, []string, error) {
	var list []any
	switch cur := existing.(type) {
	case nil:
	case []any:
		list = cur
	default:
		return nil, nil, nil, fmt.Errorf("expected a list, found %T", cur)
	}
	have := make(map[string]bool, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			have[s] = true
		}
	}
	var added, skipped []string
	for _, r := range rules {
		if have[r] {
			skipped = append(skipped, r)
			continue
		}
		list = append(list, r)
		added = append(added, r)
	}
	return list, added, skipped, nil
}

func writeJSONMap(path string, cfg map[string]any, res *agentInitResult, dryRun bool) error {
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if dryRun {
		res.preview = string(out)
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644) //nolint:gosec // agent settings hold no secrets
}
