package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"github.com/thaodangspace/tmux-window-manager/agents"
	"github.com/thaodangspace/tmux-window-manager/attachlink"
	"github.com/thaodangspace/tmux-window-manager/notify"
	"github.com/thaodangspace/tmux-window-manager/store"
	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
	"github.com/spf13/cobra"
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

// runHook does the work outside Cobra so lifecycle and optional notification
// behavior can be tested directly.
func runHook(agentName, event string, codex bool, raw []byte) {
	runHookWithNotifierAndAttach(agentName, event, codex, raw, telegramNotifierFromConfig, defaultAttachStarter)
}

// hookNotifier is the narrow delivery boundary runHook needs. Keeping it here
// lets lifecycle tests use fakes without exposing Telegram details to agents or
// store.
type hookNotifier interface {
	Send(context.Context, string) error
}

type hookNotifierFactory func() (hookNotifier, bool, error)

type hookAttach interface {
	URL() string
	Commit() error
	Cancel() error
}

type hookAttachStarter func(string) (hookAttach, error)

// runHookWithNotifier keeps existing lifecycle tests independent of the
// machine's current tmux session. Production runHook supplies the real attach
// starter; tests that inject only a notifier get the historical no-link path.
func runHookWithNotifier(agentName, event string, codex bool, raw []byte, notifierFactory hookNotifierFactory) {
	runHookWithNotifierAndAttach(agentName, event, codex, raw, notifierFactory, nil)
}

func telegramNotifierFromConfig() (hookNotifier, bool, error) {
	cfg, enabled, err := notify.LoadConfig()
	if err != nil || !enabled {
		return nil, enabled, err
	}
	return notify.NewTelegram(cfg), true, nil
}

func runHookWithNotifierAndAttach(agentName, event string, codex bool, raw []byte, notifierFactory hookNotifierFactory, attachStarter hookAttachStarter) {
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
		return
	}

	// Notification payloads do not repeat the user's prompt. Read the merged
	// row back so Telegram gets the first prompt preserved by the store.
	if !codex && h.Agent == "claude" && (h.Event == "Notification" || h.Event == "Stop") {
		persisted, found, err := db.Get(h.Agent, h.SessionID)
		if err != nil {
			debugf("hook: read notification context: %v", err)
		} else if found {
			h.Prompt = persisted.Prompt
		}
	}

	sendHookNotificationWithAttach(h, codex, notifierFactory, attachStarter)
}

// sendHookNotification preserves the no-link test/helper API. Production
// hooks use sendHookNotificationWithAttach below.
func sendHookNotification(h agents.Hook, codex bool, notifierFactory hookNotifierFactory) {
	sendHookNotificationWithAttach(h, codex, notifierFactory, nil)
}

// sendHookNotificationWithAttach performs the optional post-DB side effect and
// treats attach links as a lower-priority capability than Telegram delivery.
func sendHookNotificationWithAttach(h agents.Hook, codex bool, notifierFactory hookNotifierFactory, attachStarter hookAttachStarter) {
	if codex || h.Agent != "claude" || (h.Event != "Notification" && h.Event != "Stop") {
		return
	}
	var link hookAttach
	defer func() {
		if recovered := recover(); recovered != nil {
			if link != nil {
				_ = link.Cancel()
			}
			debugf("hook: telegram delivery failed (panic)")
		}
	}()

	if notifierFactory == nil {
		debugf("hook: telegram configuration failed (factory)")
		return
	}
	sender, enabled, err := notifierFactory()
	if err != nil {
		debugf("hook: telegram configuration failed (%s)", telegramErrorCategory(err))
		return
	}
	if !enabled {
		return
	}
	if sender == nil {
		debugf("hook: telegram configuration failed (sender)")
		return
	}

	if attachStarter != nil {
		if target := os.Getenv("TMUX_PANE"); target != "" {
			link, err = attachStarter(target)
			if err != nil {
				debugf("hook: attach link unavailable (%s)", attachErrorCategory(err))
				link = nil
			}
		}
	}

	kind := notify.Waiting
	if h.Event == "Stop" {
		kind = notify.Completed
	}
	event := notify.Event{
		Kind:      kind,
		Agent:     h.Agent,
		Cwd:       h.Cwd,
		SessionID: h.SessionID,
		Prompt:    h.Prompt,
		Detail:    h.Detail,
	}
	if link != nil {
		event.AttachURL = link.URL()
	}
	message := notify.Compose(event)
	if message == "" {
		if link != nil {
			_ = link.Cancel()
		}
		return
	}

	if err := sender.Send(context.Background(), message); err != nil {
		debugf("hook: telegram delivery failed (%s)", telegramErrorCategory(err))
		if link != nil {
			_ = link.Cancel()
		}
		return
	}
	if link != nil {
		if err := link.Commit(); err != nil {
			debugf("hook: attach helper cleanup failed")
		}
	}
}

func defaultAttachStarter(target string) (hookAttach, error) {
	if !tmuxcli.ValidPaneID(target) {
		return nil, attachlink.ErrInvalidTarget
	}
	exists, err := attachlink.ProductionTmux().PaneExists(target)
	if err != nil || !exists {
		return nil, attachlink.ErrInvalidTarget
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, attachlink.ErrStartup
	}
	return attachlink.Launch(context.Background(), executable, target)
}

func attachErrorCategory(err error) string {
	switch {
	case errors.Is(err, attachlink.ErrInvalidTarget):
		return "target"
	case errors.Is(err, attachlink.ErrStartup):
		return "startup"
	default:
		return "unknown"
	}
}

// telegramErrorCategory deliberately drops the original error text: net/http
// errors can include the token-bearing URL, and third-party/fake errors are not
// trusted to be redacted.
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
