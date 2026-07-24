package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thaodangspace/tmux-window-manager/store"
)

// withTempDB points the store at a throwaway DB for the duration of a test.
func withTempDB(t *testing.T) *store.DB {
	t.Helper()
	t.Setenv("TWM_DB_PATH", filepath.Join(t.TempDir(), "agents.db"))
	// Hook tests must never inherit credentials from either source and call Telegram.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TWM_TELEGRAM_BOT_TOKEN", "")
	t.Setenv("TWM_TELEGRAM_CHAT_ID", "")
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
	// Prompt -> running with prompt.
	runHook("claude", "UserPromptSubmit", false, []byte(`{"session_id":"s","cwd":"/w","prompt":"do it"}`))

	live, err := db.LiveByCwd()
	if err != nil {
		t.Fatal(err)
	}
	row, ok := live["/w"]
	if !ok {
		t.Fatal("no row for /w after prompt")
	}
	if row.Status != store.Running || row.Prompt != "do it" {
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

func TestRunHookSwallowsBadPayload(t *testing.T) {
	db := withTempDB(t)
	runHook("claude", "Stop", false, []byte(`garbage`)) // must not panic / write
	all, err := db.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 0 {
		t.Fatalf("bad payload wrote %d rows", len(all))
	}
}

func TestHookTelegramEligibleEventsAfterDBWrite(t *testing.T) {
	t.Run("Notification", func(t *testing.T) {
		db := withTempDB(t)
		if err := db.Upsert(store.Status{Agent: "claude", SessionID: "notify", Cwd: "/work/project", Status: store.Running, Prompt: "checkout develop", UpdatedAt: 1}); err != nil {
			t.Fatal(err)
		}
		sender := &fakeHookNotifier{}
		factoryCalls := 0
		sawPersisted := false
		factory := func() (hookNotifier, bool, error) {
			factoryCalls++
			rows, err := db.All()
			if err != nil {
				t.Fatalf("read status from notifier factory: %v", err)
			}
			sawPersisted = len(rows) == 1 && rows[0].Status == store.Waiting
			return sender, true, nil
		}

		runHookWithNotifier("claude", "Notification", false,
			[]byte(`{"session_id":"notify","cwd":"/work/project","message":"permission required"}`), factory)

		if !sawPersisted {
			t.Fatal("notifier was created before waiting status was persisted")
		}
		if factoryCalls != 1 || len(sender.messages) != 1 {
			t.Fatalf("factory calls = %d, sends = %d; want 1 each", factoryCalls, len(sender.messages))
		}
		if got, want := sender.messages[0], "🔔 Claude needs input · project\n*Session:* notify\n*Prompt:* checkout develop\n*Detail:* permission required"; got != want {
			t.Fatalf("message = %q, want %q", got, want)
		}
	})

	t.Run("Stop includes session prompt but omits assistant detail", func(t *testing.T) {
		db := withTempDB(t)
		if err := db.Upsert(store.Status{Agent: "claude", SessionID: "stop", Cwd: "/work/project", Status: store.Running, Prompt: "pull latest changes", UpdatedAt: 1}); err != nil {
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
		sender := &fakeHookNotifier{}
		factory := func() (hookNotifier, bool, error) {
			rows, err := db.All()
			if err != nil {
				t.Fatalf("read status from notifier factory: %v", err)
			}
			if len(rows) != 1 || rows[0].Latest != "work completed safely" || rows[0].Model != "Opus" {
				t.Fatalf("notifier created before enriched status persisted: %+v", rows)
			}
			return sender, true, nil
		}

		runHookWithNotifier("claude", "Stop", false, payload, factory)

		if len(sender.messages) != 1 {
			t.Fatalf("sends = %d, want 1", len(sender.messages))
		}
		if got, want := sender.messages[0], "✅ Claude finished · project\n*Session:* stop\n*Prompt:* pull latest changes"; got != want {
			t.Fatalf("message = %q, want %q", got, want)
		}
	})
}

func TestHookTelegramFiltersIneligibleEvents(t *testing.T) {
	tests := []struct {
		name  string
		agent string
		event string
		codex bool
		raw   []byte
	}{
		{name: "SessionStart", event: "SessionStart", raw: []byte(`{"session_id":"s","cwd":"/work"}`)},
		{name: "UserPromptSubmit", event: "UserPromptSubmit", raw: []byte(`{"session_id":"s","cwd":"/work","prompt":"go"}`)},
		{name: "SessionEnd", event: "SessionEnd", raw: []byte(`{"session_id":"s","cwd":"/work"}`)},
		{name: "SubagentStop", event: "SubagentStop", raw: []byte(`{"session_id":"s","cwd":"/work"}`)},
		{name: "unknown Claude event", event: "SomethingNew", raw: []byte(`{"session_id":"s","cwd":"/work"}`)},
		{name: "Codex", codex: true, raw: []byte(`{"type":"agent-turn-complete","thread-id":"t","cwd":"/work","last-assistant-message":"done"}`)},
		{name: "non-Claude agent", agent: "pi", event: "Notification", raw: []byte(`{"session_id":"s","cwd":"/work","message":"input"}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withTempDB(t)
			factoryCalls := 0
			factory := func() (hookNotifier, bool, error) {
				factoryCalls++
				return &fakeHookNotifier{}, true, nil
			}
			agent := tt.agent
			if agent == "" {
				agent = "claude"
			}
			runHookWithNotifier(agent, tt.event, tt.codex, tt.raw, factory)
			if factoryCalls != 0 {
				t.Fatalf("notifier factory called %d times, want 0", factoryCalls)
			}
		})
	}
}

func TestHookTelegramDisabledAndPartialConfiguration(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		db := withTempDB(t)
		factoryCalls := 0
		runHookWithNotifier("claude", "Notification", false,
			[]byte(`{"session_id":"s","cwd":"/work","message":"input"}`),
			func() (hookNotifier, bool, error) {
				factoryCalls++
				return nil, false, nil
			})
		if factoryCalls != 1 {
			t.Fatalf("factory calls = %d, want 1", factoryCalls)
		}
		if rows, _ := db.All(); len(rows) != 1 || rows[0].Status != store.Waiting {
			t.Fatalf("status not persisted while Telegram disabled: %+v", rows)
		}
	})

	t.Run("partial production config", func(t *testing.T) {
		db := withTempDB(t)
		logDir := t.TempDir()
		t.Setenv("TMPDIR", logDir)
		t.Setenv("TWM_HOOK_DEBUG", "1")
		t.Setenv("TWM_TELEGRAM_BOT_TOKEN", "partial-secret")
		t.Setenv("TWM_TELEGRAM_CHAT_ID", "")

		runHook("claude", "Notification", false,
			[]byte(`{"session_id":"partial","cwd":"/work","message":"input"}`))

		if rows, _ := db.All(); len(rows) != 1 || rows[0].Status != store.Waiting {
			t.Fatalf("status not persisted with partial config: %+v", rows)
		}
		log := readHookDebugLog(t, logDir)
		if !strings.Contains(log, "telegram configuration failed (partial-config)") {
			t.Fatalf("debug log missing partial-config category: %q", log)
		}
		if strings.Contains(log, "partial-secret") {
			t.Fatalf("debug log leaked token: %q", log)
		}
	})

	t.Run("malformed file config", func(t *testing.T) {
		db := withTempDB(t)
		logDir := t.TempDir()
		configDir := t.TempDir()
		t.Setenv("TMPDIR", logDir)
		t.Setenv("XDG_CONFIG_HOME", configDir)
		t.Setenv("TWM_HOOK_DEBUG", "1")
		if err := os.WriteFile(filepath.Join(configDir, "twm.toml"), []byte("[telegram\nbot_token = \"never-leak-secret\""), 0o600); err != nil {
			t.Fatal(err)
		}

		runHook("claude", "Notification", false,
			[]byte(`{"session_id":"malformed","cwd":"/work","message":"input"}`))

		if rows, _ := db.All(); len(rows) != 1 || rows[0].Status != store.Waiting {
			t.Fatalf("status not persisted with malformed config: %+v", rows)
		}
		log := readHookDebugLog(t, logDir)
		if !strings.Contains(log, "telegram configuration failed (config-file)") {
			t.Fatalf("debug log missing config-file category: %q", log)
		}
		if strings.Contains(log, "never-leak-secret") {
			t.Fatalf("debug log leaked token: %q", log)
		}
	})
}

func TestHookTelegramFailuresAreSwallowedAndRedacted(t *testing.T) {
	tests := []struct {
		name   string
		sender *fakeHookNotifier
		want   string
	}{
		{name: "error", sender: &fakeHookNotifier{err: errors.New("failed with never-leak-token")}, want: "telegram delivery failed (unknown)"},
		{name: "panic", sender: &fakeHookNotifier{panicValue: "panic with never-leak-token"}, want: "telegram delivery failed (panic)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := withTempDB(t)
			logDir := t.TempDir()
			t.Setenv("TMPDIR", logDir)
			t.Setenv("TWM_HOOK_DEBUG", "1")

			runHookWithNotifier("claude", "Notification", false,
				[]byte(`{"session_id":"failure","cwd":"/work","message":"input"}`),
				func() (hookNotifier, bool, error) { return tt.sender, true, nil })

			if rows, _ := db.All(); len(rows) != 1 || rows[0].Status != store.Waiting {
				t.Fatalf("status changed by notifier failure: %+v", rows)
			}
			log := readHookDebugLog(t, logDir)
			if !strings.Contains(log, tt.want) {
				t.Fatalf("debug log = %q, want %q", log, tt.want)
			}
			if strings.Contains(log, "never-leak-token") {
				t.Fatalf("debug log leaked notifier error: %q", log)
			}
		})
	}
}

func TestHookTelegramSkipsDeliveryWhenDBOpenFails(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TWM_DB_PATH", filepath.Join(blocker, "agents.db"))
	factoryCalls := 0

	runHookWithNotifier("claude", "Notification", false,
		[]byte(`{"session_id":"db-failure","cwd":"/work","message":"input"}`),
		func() (hookNotifier, bool, error) {
			factoryCalls++
			return &fakeHookNotifier{}, true, nil
		})

	if factoryCalls != 0 {
		t.Fatalf("notifier factory called %d times after DB failure", factoryCalls)
	}
}

type fakeHookNotifier struct {
	messages   []string
	err        error
	panicValue any
}

func (f *fakeHookNotifier) Send(_ context.Context, message string) error {
	f.messages = append(f.messages, message)
	if f.panicValue != nil {
		panic(f.panicValue)
	}
	return f.err
}

func readHookDebugLog(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "twm_hook.log"))
	if err != nil {
		t.Fatalf("read hook debug log: %v", err)
	}
	return string(data)
}
