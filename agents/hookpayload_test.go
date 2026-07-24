package agents

import (
	"testing"

	"github.com/thaodangspace/tmux-window-manager/store"
)

func TestClaudeHookEventMapping(t *testing.T) {
	tests := []struct {
		name       string
		event      string
		raw        string
		wantStatus string
		wantDelete bool
		check      func(t *testing.T, h Hook)
	}{
		{
			name:       "SessionStart -> idle",
			event:      "SessionStart",
			raw:        `{"session_id":"s1","cwd":"/w","hook_event_name":"SessionStart","source":"startup"}`,
			wantStatus: store.Idle,
		},
		{
			name:       "UserPromptSubmit -> running with prompt",
			event:      "UserPromptSubmit",
			raw:        `{"session_id":"s1","cwd":"/w","prompt":"fix   the\nbug"}`,
			wantStatus: store.Running,
			check: func(t *testing.T, h Hook) {
				if h.Prompt != "fix the bug" {
					t.Fatalf("prompt = %q, want cleaned %q", h.Prompt, "fix the bug")
				}
			},
		},
		{
			name:       "Notification -> waiting with detail",
			event:      "Notification",
			raw:        `{"session_id":"s1","cwd":"/w","message":"Claude needs your permission"}`,
			wantStatus: store.Waiting,
			check: func(t *testing.T, h Hook) {
				if h.Detail != "Claude needs your permission" {
					t.Fatalf("detail = %q", h.Detail)
				}
			},
		},
		{
			name:       "Stop -> idle keeps transcript path",
			event:      "Stop",
			raw:        `{"session_id":"s1","cwd":"/w","transcript_path":"/t/x.jsonl"}`,
			wantStatus: store.Idle,
			check: func(t *testing.T, h Hook) {
				if h.TranscriptPath != "/t/x.jsonl" {
					t.Fatalf("transcript path lost: %q", h.TranscriptPath)
				}
			},
		},
		{
			name:       "PreToolUse -> running with tool name",
			event:      "PreToolUse",
			raw:        `{"session_id":"s1","cwd":"/w","tool_name":"Bash"}`,
			wantStatus: store.Running,
			check: func(t *testing.T, h Hook) {
				if h.Detail != "Bash" {
					t.Fatalf("detail = %q", h.Detail)
				}
			},
		},
		{
			name:       "SessionEnd -> delete",
			event:      "SessionEnd",
			raw:        `{"session_id":"s1","cwd":"/w","reason":"exit"}`,
			wantDelete: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h, ok := ClaudeHook("claude", tc.event, []byte(tc.raw))
			if !ok {
				t.Fatal("ClaudeHook returned ok=false")
			}
			if h.SessionID != "s1" || h.Cwd != "/w" {
				t.Fatalf("base fields wrong: %+v", h)
			}
			if h.Delete != tc.wantDelete {
				t.Fatalf("delete = %v, want %v", h.Delete, tc.wantDelete)
			}
			if !tc.wantDelete && h.Status != tc.wantStatus {
				t.Fatalf("status = %q, want %q", h.Status, tc.wantStatus)
			}
			if tc.check != nil {
				tc.check(t, h)
			}
		})
	}
}

func TestClaudeHookEventFromBodyWhenArgEmpty(t *testing.T) {
	h, ok := ClaudeHook("claude", "", []byte(`{"session_id":"s","hook_event_name":"Notification","message":"hi"}`))
	if !ok || h.Status != store.Waiting {
		t.Fatalf("body event not used: ok=%v status=%q", ok, h.Status)
	}
}

func TestClaudeHookRejectsBadPayload(t *testing.T) {
	if _, ok := ClaudeHook("claude", "Stop", []byte(`not json`)); ok {
		t.Fatal("expected ok=false for malformed json")
	}
	if _, ok := ClaudeHook("claude", "Stop", []byte(`{"cwd":"/w"}`)); ok {
		t.Fatal("expected ok=false when session_id missing")
	}
}

func TestCodexHook(t *testing.T) {
	raw := `{"type":"agent-turn-complete","thread-id":"t-9","cwd":"/proj",
		"input-messages":["please   refactor"],"last-assistant-message":"done\nrefactoring"}`
	h, ok := CodexHook([]byte(raw))
	if !ok {
		t.Fatal("CodexHook ok=false")
	}
	if h.Agent != "codex" || h.SessionID != "t-9" || h.Cwd != "/proj" {
		t.Fatalf("base fields: %+v", h)
	}
	if h.Status != store.Idle {
		t.Fatalf("status = %q", h.Status)
	}
	if h.Prompt != "please refactor" {
		t.Fatalf("prompt = %q", h.Prompt)
	}
	if h.Latest != "done refactoring" {
		t.Fatalf("latest = %q", h.Latest)
	}
}

func TestCodexHookRejectsBad(t *testing.T) {
	if _, ok := CodexHook([]byte(`{}`)); ok {
		t.Fatal("expected ok=false without thread-id")
	}
}
