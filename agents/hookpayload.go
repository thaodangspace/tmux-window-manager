package agents

import (
	"encoding/json"

	"github.com/thaodangspace/tmux-window-manager/store"
)

// Hook is the vendor-neutral form of a single lifecycle event, normalized from
// either a Claude Code hook (stdin JSON) or a Codex notify payload (argv JSON).
// The cli/hook command turns it into a store.Status write.
type Hook struct {
	Agent          string
	SessionID      string
	Event          string // original vendor event name (for the history table)
	Cwd            string
	TranscriptPath string // Claude only; the command tail-reads it on Stop
	Prompt         string
	Model          string
	Latest         string
	Detail         string
	Status         string // mapped store status (idle/running/waiting)
	Delete         bool   // true => remove the row instead of upserting (SessionEnd)
}

// claudeHookInput is the subset of Claude Code's hook stdin JSON we read. Unknown
// fields are ignored, so schema additions never break us.
type claudeHookInput struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	HookEventName  string `json:"hook_event_name"`
	Prompt         string `json:"prompt"`    // UserPromptSubmit
	Message        string `json:"message"`   // Notification
	ToolName       string `json:"tool_name"` // Pre/PostToolUse
}

// ClaudeHook normalizes a Claude Code hook payload. event is taken from argv
// (authoritative) and falls back to the body's hook_event_name. ok is false
// when the payload is unparseable or carries no session id.
func ClaudeHook(agent, event string, raw []byte) (Hook, bool) {
	var in claudeHookInput
	if json.Unmarshal(raw, &in) != nil {
		return Hook{}, false
	}
	if event == "" {
		event = in.HookEventName
	}
	if in.SessionID == "" {
		return Hook{}, false
	}
	h := Hook{
		Agent:          agent,
		SessionID:      in.SessionID,
		Event:          event,
		Cwd:            in.Cwd,
		TranscriptPath: in.TranscriptPath,
	}
	switch event {
	case "SessionStart":
		h.Status = store.Idle
	case "UserPromptSubmit":
		h.Status = store.Running
		h.Prompt = clean(in.Prompt)
	case "Notification":
		h.Status = store.Waiting
		h.Detail = clean(in.Message)
	case "PreToolUse", "PostToolUse":
		h.Status = store.Running
		h.Detail = in.ToolName
	case "Stop", "SubagentStop":
		h.Status = store.Idle
	case "SessionEnd":
		h.Delete = true
	default:
		// Unknown event: keep the session alive as idle rather than guess.
		h.Status = store.Idle
	}
	return h, true
}

// codexNotify is Codex's notify payload (passed as a single argv JSON string).
type codexNotify struct {
	Type             string   `json:"type"`
	ThreadID         string   `json:"thread-id"`
	Cwd              string   `json:"cwd"`
	InputMessages    []string `json:"input-messages"`
	LastAssistantMsg string   `json:"last-assistant-message"`
}

// CodexHook normalizes a Codex notify payload. Currently Codex emits
// agent-turn-complete, which maps to idle with a refreshed latest message.
func CodexHook(raw []byte) (Hook, bool) {
	var in codexNotify
	if json.Unmarshal(raw, &in) != nil {
		return Hook{}, false
	}
	if in.ThreadID == "" {
		return Hook{}, false
	}
	h := Hook{
		Agent:     "codex",
		SessionID: in.ThreadID,
		Event:     in.Type,
		Cwd:       in.Cwd,
		Latest:    clean(in.LastAssistantMsg),
		Status:    store.Idle,
	}
	if len(in.InputMessages) > 0 {
		h.Prompt = clean(in.InputMessages[0])
	}
	return h, true
}
