package cli

import (
	"path/filepath"
	"testing"

	"github.com/dtonair/tmux-window-manager/store"
)

// withTempDB points the store at a throwaway DB for the duration of a test.
func withTempDB(t *testing.T) *store.DB {
	t.Helper()
	t.Setenv("TWM_DB_PATH", filepath.Join(t.TempDir(), "agents.db"))
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
