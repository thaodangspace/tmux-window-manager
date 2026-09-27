package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/tmux-window-manager/agents"
	"github.com/thaodangspace/tmux-window-manager/notify"
	"github.com/thaodangspace/tmux-window-manager/store"
	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
)

// newHookCommand handles a single agent lifecycle event: it reads the vendor
// payload (Claude on stdin, Codex via --codex), normalizes it, and writes one
// status row. It is invoked by the hooks `twm install-hooks` configures.
//
// Hard rule: it must NEVER fail the agent. Work is bounded, every error path
// logs only when TWM_HOOK_DEBUG is set, and the command returns nil so the
// process exits 0.
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

// runHook does the work outside Cobra so lifecycle behavior can be tested directly.
func runHook(agentName, event string, codex bool, raw []byte) {
	runHookWithNotifier(agentName, event, codex, raw, telegramNotifierFromConfig)
}

type hookNotifier interface {
	Send(ctx context.Context, text string, silent bool) error
}

type hookNotifierFactory func() (hookNotifier, notify.Options, bool, error)

func telegramNotifierFromConfig() (hookNotifier, notify.Options, bool, error) {
	cfg, enabled, err := notify.LoadConfig()
	if err != nil || !enabled {
		return nil, notify.Options{}, enabled, err
	}
	return notify.NewTelegram(cfg), cfg.Options, true, nil
}

// focusRecent is how recently a tmux client must have had input for a visible
// agent pane to count as watched.
const focusRecent = 2 * time.Minute

// Test seams for the hook's view of time and of tmux.
var (
	hookNow        = time.Now
	hookLookupPane = func(pane string) (tmuxcli.PaneFocus, bool) {
		return tmuxcli.LookupPane(pane, hookNow(), focusRecent)
	}
)

func runHookWithNotifier(agentName, event string, codex bool, raw []byte, notifierFactory hookNotifierFactory) {
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

	now := hookNow().UnixMilli()
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
		UpdatedAt: now,
		Event:     h.Event,
	}
	if h.Event == "UserPromptSubmit" {
		s.TurnPrompt = h.Prompt
		s.TurnStartedAt = now
	}
	if err := db.Upsert(s); err != nil {
		debugf("hook: upsert: %v", err)
		return
	}

	if codex || (h.Event != "Notification" && h.Event != "Stop") {
		return
	}
	// Notification payloads do not repeat the user's prompt, and turn timing
	// lives only in the store. Read the merged row back for that context.
	persisted, found, err := db.Get(h.Agent, h.SessionID)
	if err != nil || !found {
		debugf("hook: read notification context: found=%v err=%v", found, err)
		return
	}
	if sendHookNotification(h, persisted, notifierFactory) {
		if err := db.MarkNotified(h.Agent, h.SessionID, hookNow().UnixMilli()); err != nil {
			debugf("hook: mark notified: %v", err)
		}
	}
}

// sendHookNotification sends user-attention and completed-turn events for
// Claude-style hooks, applying the user's noise filters. Delivery is best
// effort, happens after the status write, and reports whether a message was
// delivered.
func sendHookNotification(h agents.Hook, row store.Status, notifierFactory hookNotifierFactory) (sent bool) {
	defer func() {
		if recover() != nil {
			debugf("hook: telegram delivery failed (panic)")
			sent = false
		}
	}()
	if notifierFactory == nil {
		return false
	}
	sender, opts, enabled, err := notifierFactory()
	if err != nil {
		debugf("hook: telegram configuration failed (%s)", telegramErrorCategory(err))
		return false
	}
	if !enabled || sender == nil {
		return false
	}

	now := hookNow()
	var turn time.Duration
	if row.TurnStartedAt > 0 {
		turn = now.Sub(time.UnixMilli(row.TurnStartedAt))
	}
	if reason := suppressReason(h, row, opts, turn); reason != "" {
		debugf("hook: telegram skipped (%s)", reason)
		return false
	}
	pane, _ := hookLookupPane(os.Getenv("TMUX_PANE"))
	if opts.SkipWhenFocused && pane.Watched {
		debugf("hook: telegram skipped (pane is focused)")
		return false
	}

	prompt := row.TurnPrompt
	if prompt == "" {
		prompt = row.Prompt
	}
	event := notify.Event{
		Agent:     h.Agent,
		Cwd:       h.Cwd,
		SessionID: h.SessionID,
		Location:  pane.Location,
		Model:     row.Model,
		Prompt:    prompt,
	}
	if h.Event == "Stop" {
		event.Kind = notify.Completed
		event.Duration = turn
		if opts.IncludeResponse {
			event.Response = row.Latest
		}
	} else {
		event.Kind = notify.Waiting
		event.Detail = h.Detail
	}
	message := notify.Compose(event)
	if message == "" {
		return false
	}
	// Only a blocked agent should buzz the phone; a finished turn arrives quietly.
	if err := sender.Send(context.Background(), message, event.Kind == notify.Completed); err != nil {
		debugf("hook: telegram delivery failed (%s)", telegramErrorCategory(err))
		return false
	}
	return true
}

// suppressReason returns why an event should not be delivered, or "" to send.
//   - A Stop for a turn shorter than Options.MinTurn is noise: the user was
//     most likely still at the keyboard.
//   - Claude's idle reminder repeats the preceding Stop, so it is sent only
//     when nothing was delivered for the current turn (e.g. the Stop was
//     suppressed as short and the user has since walked away).
func suppressReason(h agents.Hook, row store.Status, opts notify.Options, turn time.Duration) string {
	switch {
	case h.Event == "Stop" && opts.MinTurn > 0 && row.TurnStartedAt > 0 && turn < opts.MinTurn:
		return "short turn"
	case h.Event == "Notification" && h.IsIdlePrompt() &&
		(row.TurnStartedAt == 0 || row.NotifiedAt >= row.TurnStartedAt):
		return "idle reminder already covered"
	}
	return ""
}

// telegramErrorCategory deliberately drops the original error text because it
// may contain a token-bearing URL or another secret.
func telegramErrorCategory(err error) string {
	switch {
	case errors.Is(err, notify.ErrPartialConfig):
		return "partial-config"
	case errors.Is(err, notify.ErrConfigFile):
		return "config-file"
	case errors.Is(err, notify.ErrRequest):
		return "request"
	case errors.Is(err, notify.ErrTransport):
		return "transport"
	case errors.Is(err, notify.ErrHTTP):
		return "http"
	case errors.Is(err, notify.ErrResponse):
		return "response"
	case errors.Is(err, notify.ErrRejected):
		return "rejected"
	default:
		return "unknown"
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
