package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thaodangspace/tmux-window-manager/notify"
	"github.com/thaodangspace/tmux-window-manager/store"
	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
)

// withTempDB points the store at a throwaway DB for the duration of a test.
func withTempDB(t *testing.T) *store.DB {
	t.Helper()
	t.Setenv("TWM_DB_PATH", filepath.Join(t.TempDir(), "agents.db"))
	// Tests must never inherit real Telegram credentials.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TWM_TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TWM_TELEGRAM_CHAT_ID", "")
	// Nor the real tmux pane of whoever runs the tests.
	t.Setenv("TMUX_PANE", "")
	db, err := store.Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestRunHookLifecycle(t *testing.T) {
	db := withTempDB(t)

	// Start -> idle row exists.
	runHook("claude", "SessionStart", false, []byte(`{"session_id":"s","cwd":"/w"}`))
	// Prompt -> working with prompt.
	runHook("claude", "UserPromptSubmit", false, []byte(`{"session_id":"s","cwd":"/w","prompt":"do it"}`))

	live, err := db.LiveByCwd()
	if err != nil {
		t.Fatal(err)
	}
	row, ok := live["/w"]
	if !ok {
		t.Fatal("no row for /w after prompt")
	}
	if row.Status != store.Working || row.Prompt != "do it" {
		t.Fatalf("after prompt: %+v", row)
	}

	// Notification -> waiting, prompt preserved.
	runHook("claude", "Notification", false, []byte(`{"session_id":"s","cwd":"/w","message":"need input"}`))
	live, _ = db.LiveByCwd()
	if row = live["/w"]; row.Status != store.Waiting || row.Detail != "need input" || row.Prompt != "do it" {
		t.Fatalf("after notification: %+v", row)
	}

	// SessionEnd -> row deleted.
	runHook("claude", "SessionEnd", false, []byte(`{"session_id":"s","cwd":"/w","reason":"exit"}`))
	live, _ = db.LiveByCwd()
	if _, ok := live["/w"]; ok {
		t.Fatal("row should be deleted after SessionEnd")
	}
}

func TestRunHookCodex(t *testing.T) {
	db := withTempDB(t)
	runHook("codex", "", true, []byte(`{"type":"agent-turn-complete","thread-id":"t","cwd":"/c","last-assistant-message":"finished"}`))
	live, _ := db.LiveByCwd()
	row, ok := live["/c"]
	if !ok {
		t.Fatal("no codex row")
	}
	if row.Agent != "codex" || row.Status != store.Idle || row.Latest != "finished" {
		t.Fatalf("codex row: %+v", row)
	}
}

func TestRunHookStopEnrichment(t *testing.T) {
	db := withTempDB(t)
	if err := db.Upsert(store.Status{Agent: "claude", SessionID: "stop", Cwd: "/work/project", Status: store.Working, Prompt: "pull latest changes", UpdatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	transcript := filepath.Join(t.TempDir(), "session.jsonl")
	line := `{"type":"assistant","timestamp":"2026-07-19T10:00:00Z","model":"Opus","message":{"role":"assistant","content":"work completed safely"}}` + "\n"
	if err := os.WriteFile(transcript, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]string{
		"session_id":      "stop",
		"cwd":             "/work/project",
		"transcript_path": transcript,
	})
	if err != nil {
		t.Fatal(err)
	}

	runHook("claude", "Stop", false, payload)

	rows, err := db.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Latest != "work completed safely" || rows[0].Model != "Opus" || rows[0].Prompt != "pull latest changes" {
		t.Fatalf("unexpected rows after stop: %+v", rows)
	}
}

type fakeNotifier struct {
	messages []string
	silent   []bool
	err      error
}

func (f *fakeNotifier) Send(_ context.Context, message string, silent bool) error {
	f.messages = append(f.messages, message)
	f.silent = append(f.silent, silent)
	return f.err
}

func (f *fakeNotifier) factory(opts notify.Options) hookNotifierFactory {
	return func() (hookNotifier, notify.Options, bool, error) { return f, opts, true, nil }
}

// withClock pins hookNow to a controllable time for the duration of a test.
func withClock(t *testing.T) *time.Time {
	t.Helper()
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	prev := hookNow
	hookNow = func() time.Time { return now }
	t.Cleanup(func() { hookNow = prev })
	return &now
}

// withPane fakes the tmux pane lookup.
func withPane(t *testing.T, focus tmuxcli.PaneFocus) {
	t.Helper()
	t.Setenv("TMUX_PANE", "%1")
	prev := hookLookupPane
	hookLookupPane = func(pane string) (tmuxcli.PaneFocus, bool) {
		if pane != "%1" {
			t.Errorf("looked up pane %q, want %%1", pane)
		}
		return focus, true
	}
	t.Cleanup(func() { hookLookupPane = prev })
}

const (
	payloadPrompt1 = `{"session_id":"s","cwd":"/work/project","prompt":"first task"}`
	payloadPrompt2 = `{"session_id":"s","cwd":"/work/project","prompt":"second task"}`
	payloadStop    = `{"session_id":"s","cwd":"/work/project"}`
	payloadAsk     = `{"session_id":"s","cwd":"/work/project","message":"permission required","notification_type":"permission_prompt"}`
	payloadIdle    = `{"session_id":"s","cwd":"/work/project","message":"Claude is waiting for your input","notification_type":"idle_prompt"}`
)

func TestHookTelegramOnlyForAskAndStop(t *testing.T) {
	withTempDB(t)
	now := withClock(t)
	sender := &fakeNotifier{}
	factory := sender.factory(notify.DefaultOptions())

	runHookWithNotifier("pi", "UserPromptSubmit", false, []byte(payloadPrompt1), factory)
	if len(sender.messages) != 0 {
		t.Fatalf("prompt produced Telegram message: %q", sender.messages)
	}

	*now = now.Add(time.Minute)
	runHookWithNotifier("pi", "Notification", false, []byte(payloadAsk), factory)
	*now = now.Add(4*time.Minute + 12*time.Second)
	runHookWithNotifier("pi", "Stop", false, []byte(payloadStop), factory)
	if len(sender.messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(sender.messages))
	}
	if !strings.Contains(sender.messages[0], "Pi needs input") || !strings.Contains(sender.messages[0], "permission required") {
		t.Fatalf("waiting message = %q", sender.messages[0])
	}
	if !strings.Contains(sender.messages[1], "Pi finished · project · 5m12s") {
		t.Fatalf("stop message = %q", sender.messages[1])
	}
	if sender.silent[0] || !sender.silent[1] {
		t.Fatalf("silent = %v, want waiting loud and completed silent", sender.silent)
	}
}

func TestHookTelegramSkipsShortTurns(t *testing.T) {
	withTempDB(t)
	now := withClock(t)
	sender := &fakeNotifier{}

	runHookWithNotifier("claude", "UserPromptSubmit", false, []byte(payloadPrompt1), sender.factory(notify.DefaultOptions()))
	*now = now.Add(10 * time.Second)
	runHookWithNotifier("claude", "Stop", false, []byte(payloadStop), sender.factory(notify.DefaultOptions()))
	if len(sender.messages) != 0 {
		t.Fatalf("short turn notified: %q", sender.messages)
	}

	// MinTurn 0 sends every completed turn.
	runHookWithNotifier("claude", "Stop", false, []byte(payloadStop), sender.factory(notify.Options{}))
	if len(sender.messages) != 1 {
		t.Fatalf("messages = %d with MinTurn 0, want 1", len(sender.messages))
	}
}

func TestHookTelegramIdleReminderOnlyWhenTurnUnreported(t *testing.T) {
	withTempDB(t)
	now := withClock(t)
	sender := &fakeNotifier{}
	factory := sender.factory(notify.DefaultOptions())

	// Long turn: the Stop is delivered, so the idle reminder is a duplicate.
	runHookWithNotifier("claude", "UserPromptSubmit", false, []byte(payloadPrompt1), factory)
	*now = now.Add(time.Minute)
	runHookWithNotifier("claude", "Stop", false, []byte(payloadStop), factory)
	*now = now.Add(time.Minute)
	runHookWithNotifier("claude", "Notification", false, []byte(payloadIdle), factory)
	if len(sender.messages) != 1 {
		t.Fatalf("messages = %d, want only the Stop", len(sender.messages))
	}

	// Short turn: the Stop is suppressed, so the reminder is the only signal.
	*now = now.Add(time.Minute)
	runHookWithNotifier("claude", "UserPromptSubmit", false, []byte(payloadPrompt2), factory)
	*now = now.Add(5 * time.Second)
	runHookWithNotifier("claude", "Stop", false, []byte(payloadStop), factory)
	*now = now.Add(time.Minute)
	runHookWithNotifier("claude", "Notification", false, []byte(payloadIdle), factory)
	if len(sender.messages) != 2 || !strings.Contains(sender.messages[1], "needs input") ||
		!strings.Contains(sender.messages[1], "second task") {
		t.Fatalf("messages = %q, want the idle reminder for the second task", sender.messages)
	}

	// A second reminder for the same turn is not repeated.
	runHookWithNotifier("claude", "Notification", false, []byte(payloadIdle), factory)
	if len(sender.messages) != 2 {
		t.Fatalf("idle reminder repeated: %q", sender.messages)
	}
}

func TestHookTelegramSkipsFocusedPane(t *testing.T) {
	withTempDB(t)
	now := withClock(t)
	withPane(t, tmuxcli.PaneFocus{Location: "work:3", Watched: true})
	sender := &fakeNotifier{}

	runHookWithNotifier("claude", "UserPromptSubmit", false, []byte(payloadPrompt1), sender.factory(notify.DefaultOptions()))
	*now = now.Add(time.Minute)
	runHookWithNotifier("claude", "Notification", false, []byte(payloadAsk), sender.factory(notify.DefaultOptions()))
	runHookWithNotifier("claude", "Stop", false, []byte(payloadStop), sender.factory(notify.DefaultOptions()))
	if len(sender.messages) != 0 {
		t.Fatalf("focused pane notified: %q", sender.messages)
	}

	opts := notify.DefaultOptions()
	opts.SkipWhenFocused = false
	runHookWithNotifier("claude", "Stop", false, []byte(payloadStop), sender.factory(opts))
	if len(sender.messages) != 1 || !strings.Contains(sender.messages[0], "*Where:* work:3") {
		t.Fatalf("messages = %q, want one with the tmux location", sender.messages)
	}
}

func TestHookTelegramUsesTurnPromptAndOptInResponse(t *testing.T) {
	withTempDB(t)
	now := withClock(t)
	transcript := filepath.Join(t.TempDir(), "session.jsonl")
	line := `{"type":"assistant","timestamp":"2026-09-27T10:00:00Z","model":"Opus","message":{"role":"assistant","content":"all tests pass"}}` + "\n"
	if err := os.WriteFile(transcript, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	stop, _ := json.Marshal(map[string]string{"session_id": "s", "cwd": "/work/project", "transcript_path": transcript})
	sender := &fakeNotifier{}

	runHookWithNotifier("claude", "UserPromptSubmit", false, []byte(payloadPrompt1), sender.factory(notify.Options{}))
	runHookWithNotifier("claude", "UserPromptSubmit", false, []byte(payloadPrompt2), sender.factory(notify.Options{}))
	*now = now.Add(time.Minute)
	runHookWithNotifier("claude", "Stop", false, stop, sender.factory(notify.Options{}))
	runHookWithNotifier("claude", "Stop", false, stop, sender.factory(notify.Options{IncludeResponse: true}))
	if len(sender.messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(sender.messages))
	}
	for _, m := range sender.messages {
		if !strings.Contains(m, "*Prompt:* second task") || !strings.Contains(m, "*Model:* Opus") {
			t.Fatalf("message lacks current turn prompt or model: %q", m)
		}
	}
	if strings.Contains(sender.messages[0], "all tests pass") {
		t.Fatalf("response included without opt-in: %q", sender.messages[0])
	}
	if !strings.Contains(sender.messages[1], "*Response:* all tests pass") {
		t.Fatalf("opt-in response missing: %q", sender.messages[1])
	}
}

func TestRunHookSwallowsBadPayload(t *testing.T) {
	db := withTempDB(t)
	logDir := t.TempDir()
	t.Setenv("TMPDIR", logDir)
	t.Setenv("TWM_HOOK_DEBUG", "1")

	runHook("claude", "Stop", false, []byte(`garbage`)) // must not panic / write
	all, err := db.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Fatalf("bad payload wrote %d rows", len(all))
	}
	log := readHookDebugLog(t, logDir)
	if !strings.Contains(log, "hook: unparseable payload") {
		t.Fatalf("debug log missing unparseable payload: %q", log)
	}
}

func readHookDebugLog(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "twm_hook.log"))
	if err != nil {
		t.Fatalf("read hook debug log: %v", err)
	}
	return string(data)
}
