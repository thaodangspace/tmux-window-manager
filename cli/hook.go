package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/dtonair/tmux-window-manager/agents"
	"github.com/dtonair/tmux-window-manager/store"
	"github.com/spf13/cobra"
)

// newHookCommand handles a single agent lifecycle event: it reads the vendor
// payload (Claude on stdin, Codex via --codex), normalizes it, and writes one
// status row. It is invoked by the hooks `twm install-hooks` configures.
//
// Hard rule: it must NEVER block or fail the agent. Every error path logs (only
// when TWM_HOOK_DEBUG is set) and returns nil so the process exits 0.
func newHookCommand() *cobra.Command {
	var (
		agentName string
		codex     bool
	)
	cmd := &cobra.Command{
		Use:    "hook [event]",
		Short:  "Record an agent lifecycle event (called from Claude/Codex hooks)",
		Args:   cobra.MaximumNArgs(1),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			raw, _ := io.ReadAll(cmd.InOrStdin())
			event := ""
			if len(args) > 0 {
				event = args[0]
			}
			runHook(agentName, event, codex, raw)
			return nil // always succeed
		},
	}
	cmd.Flags().StringVar(&agentName, "agent", "claude", "agent that fired the hook")
	cmd.Flags().BoolVar(&codex, "codex", false, "parse the payload as a Codex notify message")
	return cmd
}

// runHook does the work, isolated from cobra so it is unit-testable and so any
// panic/error is swallowed by the caller.
func runHook(agentName, event string, codex bool, raw []byte) {
	var (
		h  agents.Hook
		ok bool
	)
	if codex {
		h, ok = agents.CodexHook(raw)
	} else {
		h, ok = agents.ClaudeHook(agentName, event, raw)
	}
	if !ok {
		debugf("hook: unparseable payload (codex=%v event=%q): %s", codex, event, raw)
		return
	}

	// Resolve the agent process this hook belongs to (the hook runs as a child
	// of the agent), so the reader can gate on its liveness. The agent identity
	// itself comes from the payload/--agent flag (authoritative); the walk only
	// supplies the pid.
	pid, _ := agents.NewDetector().NearestAgent(strconv.Itoa(os.Getppid()))

	// On idle (turn finished) refresh model + latest from the transcript tail.
	if h.Status == store.Idle && h.TranscriptPath != "" {
		if m, l := agents.TranscriptTail(h.TranscriptPath); m != "" || l != "" {
			if m != "" {
				h.Model = m
			}
			if l != "" {
				h.Latest = l
			}
		}
	}

	db, err := store.Open()
	if err != nil {
		debugf("hook: open db: %v", err)
		return
	}
	defer db.Close()

	if h.Delete {
		if err := db.Delete(h.Agent, h.SessionID); err != nil {
			debugf("hook: delete: %v", err)
		}
		return
	}

	s := store.Status{
		Agent:     h.Agent,
		SessionID: h.SessionID,
		Cwd:       h.Cwd,
		Pid:       pid,
		Status:    h.Status,
		Detail:    h.Detail,
		Model:     h.Model,
		Prompt:    h.Prompt,
		Latest:    h.Latest,
		UpdatedAt: time.Now().UnixMilli(),
		Event:     h.Event,
	}
	if err := db.Upsert(s); err != nil {
		debugf("hook: upsert: %v", err)
	}
}

// debugf appends a line to $TMPDIR/twm_hook.log when TWM_HOOK_DEBUG is set.
// Silent otherwise so hooks add no noise to a normal agent session.
func debugf(format string, a ...any) {
	if os.Getenv("TWM_HOOK_DEBUG") == "" {
		return
	}
	f, err := os.OpenFile(os.TempDir()+"/twm_hook.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, format+"\n", a...)
}
