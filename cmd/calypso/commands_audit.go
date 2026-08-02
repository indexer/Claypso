package main

import (
	"fmt"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/yemon/calypso/internal/audit"
	"golang.org/x/term"
)

// auditLogPath keeps the log next to the vault file, so per-vault logs stay
// separate and tests with --vault tempdirs are hermetic automatically.
func auditLogPath() string { return vaultPath + ".audit.log" }

// recordAudit appends one event describing a gate decision or control
// operation. err is the gate's verdict (nil = permitted). Best-effort: a
// failing audit write warns on stderr but never blocks the user's command.
// CALYPSO_AUDIT=0 disables logging entirely.
func recordAudit(op, spec string, keys int, gateErr error) {
	if os.Getenv("CALYPSO_AUDIT") == "0" {
		return
	}
	outcome := "ok"
	if gateErr != nil {
		outcome = "denied"
	}
	ev := audit.Event{
		Time:       time.Now().UTC().Format(time.RFC3339),
		Op:         op,
		Spec:       spec,
		Keys:       keys,
		Outcome:    outcome,
		User:       currentUsername(),
		ParentProc: parentProcName(),
		TTY:        term.IsTerminal(int(os.Stdin.Fd())), //nolint:gosec // fd fits in int on supported platforms
		Unattended: os.Getenv(unattendedRevealEnv) == "1",
	}
	if err := audit.Append(auditLogPath(), ev); err != nil {
		fmt.Fprintf(os.Stderr, "warning: audit log write failed: %v\n", err)
	}
}

func currentUsername() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

// parentProcName best-effort identifies what invoked calypso (a shell, an
// agent harness, a CI runner). Linux only; other platforms return "".
func parentProcName() string {
	comm, err := os.ReadFile("/proc/" + strconv.Itoa(os.Getppid()) + "/comm")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(comm))
}

func auditCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "audit",
		Short: "Inspect the tamper-evident log of secret-touching operations",
		Long: `Every reveal-class gate decision (permitted or denied) and control
operation (lockdown, rekey, sync) appends a hash-chained record to
<vault>.audit.log. Records hold metadata only — never values or
passphrases. Denied attempts are logged too, so an agent probing
--reveal leaves a trace.

Set CALYPSO_AUDIT=0 to disable logging.`,
	}
	cmd.AddCommand(auditListCmd(), auditVerifyCmd())
	return cmd
}

func auditListCmd() *cobra.Command {
	var limit int
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show recent audit events",
		RunE: func(cmd *cobra.Command, args []string) error {
			events, err := audit.Read(auditLogPath())
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Println("No audit events yet.")
					return nil
				}
				return err
			}
			if limit > 0 && len(events) > limit {
				events = events[len(events)-limit:]
			}
			w := newTabWriter()
			fmt.Fprintln(w, "TIME\tOP\tSPEC\tKEYS\tOUTCOME\tUSER\tPARENT\tUNATTENDED")
			for _, e := range events {
				spec := e.Spec
				if spec == "" {
					spec = "-"
				}
				parent := e.ParentProc
				if parent == "" {
					parent = "-"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\t%s\t%v\n",
					e.Time, e.Op, spec, e.Keys, e.Outcome, e.User, parent, e.Unattended)
			}
			return w.Flush()
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "show only the most recent N events (0 = all)")
	return cmd
}

func auditVerifyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "verify",
		Short: "Check the audit log's hash chain for tampering",
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := audit.Verify(auditLogPath())
			if err != nil {
				if os.IsNotExist(err) {
					fmt.Println("No audit log yet — nothing to verify.")
					return nil
				}
				return fmt.Errorf("audit chain INVALID after %d good event(s): %w", n, err)
			}
			fmt.Printf("Audit chain OK: %d event(s) verified.\n", n)
			return nil
		},
	}
}
