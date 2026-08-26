package cli

import (
	"encoding/json"
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

func TestRunHookStopEnrichment(t *testing.T) {
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

	runHook("claude", "Stop", false, payload)

	rows, err := db.All()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Latest != "work completed safely" || rows[0].Model != "Opus" || rows[0].Prompt != "pull latest changes" {
		t.Fatalf("unexpected rows after stop: %+v", rows)
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
