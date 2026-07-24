package picker

import (
	"testing"

	"github.com/thaodangspace/tmux-window-manager/agents"
	"github.com/thaodangspace/tmux-window-manager/store"
	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
)

// TestWindowBadgeRequiresLiveAgentProcess guards against lighting up unrelated
// shells: two windows share a directory, but only the window whose pane actually
// runs `claude` gets the badge (the bare shell has no detected agent process).
func TestWindowBadgeRequiresLiveAgentProcess(t *testing.T) {
	live := []store.Status{
		{Agent: "claude", Model: "opus", Status: store.Running, Pid: 101, Cwd: "/code/foo"},
	}
	panes := []tmuxcli.Pane{
		{Session: "cli", WindowIndex: 1, PID: "100", Path: "/code/foo"}, // runs claude
		{Session: "cli", WindowIndex: 2, PID: "200", Path: "/code/foo"}, // bare shell
	}
	// pid 100's shell has a claude child (pid 101); pid 200's shell only runs go.
	det := agents.NewDetectorFromSnapshot("100 1 zsh\n101 100 claude\n200 1 zsh\n201 200 go\n")
	e := newLiveEnricher(live, panes, det)

	if b := e.Window("cli", 1, "", ""); b.AgentLabel != "claude(opus)" || b.PaneLabel != "claude" {
		t.Errorf("window 1 badge = %q/%q, want %q/%q", b.AgentLabel, b.PaneLabel, "claude(opus)", "claude")
	}
	if b := e.Window("cli", 2, "", ""); b.AgentLabel != "" {
		t.Errorf("window 2 (bare shell) should have no badge, got %q", b.AgentLabel)
	}
}

// TestTwoAgentsSameDirStayDistinct is the regression for the reported bug: a
// codex window and a claude window in the same directory must show their own
// agent, not collapse onto whichever reported last (which cwd matching did).
func TestTwoAgentsSameDirStayDistinct(t *testing.T) {
	live := []store.Status{
		{Agent: "codex", Status: store.Idle, Pid: 301, Cwd: "/code/cli"},
		{Agent: "claude", Model: "claude-opus-4-8", Status: store.Running, Pid: 401, Cwd: "/code/cli"},
	}
	panes := []tmuxcli.Pane{
		{Session: "cli", WindowIndex: 1, PID: "300", Path: "/code/cli"},
		{Session: "cli", WindowIndex: 2, PID: "400", Path: "/code/cli"},
	}
	det := agents.NewDetectorFromSnapshot("300 1 zsh\n301 300 codex\n400 1 zsh\n401 400 claude\n")
	e := newLiveEnricher(live, panes, det)

	if b := e.Window("cli", 1, "", ""); b.AgentLabel != "codex" || b.PaneLabel != "codex" {
		t.Errorf("window 1 badge = %q/%q, want %q/%q", b.AgentLabel, b.PaneLabel, "codex", "codex")
	}
	if b := e.Window("cli", 2, "", ""); b.AgentLabel != "claude(claude-opus-4-8)" || b.PaneLabel != "claude" {
		t.Errorf("window 2 badge = %q/%q, want %q/%q", b.AgentLabel, b.PaneLabel, "claude(claude-opus-4-8)", "claude")
	}
}

// TestBadgeFromDetectorWithoutStatusRow covers a running agent whose hooks have
// not reported (e.g. a fresh Codex session): the detected process name alone
// still produces a badge, rather than falling back to the raw pane command.
func TestBadgeFromDetectorWithoutStatusRow(t *testing.T) {
	panes := []tmuxcli.Pane{
		{Session: "cli", WindowIndex: 1, PID: "500", Path: "/code/cli"},
	}
	det := agents.NewDetectorFromSnapshot("500 1 zsh\n501 500 codex\n")
	e := newLiveEnricher(nil, panes, det)

	b := e.Window("cli", 1, "", "")
	if b.AgentLabel != "codex" || b.PaneLabel != "codex" {
		t.Errorf("badge = %q/%q, want %q/%q", b.AgentLabel, b.PaneLabel, "codex", "codex")
	}
	if b.Status != "" {
		t.Errorf("status = %q, want empty (no status row yet)", b.Status)
	}
}

func TestEnrichPicksMostUrgentStatus(t *testing.T) {
	e := &LiveEnricher{live: []store.Status{
		{Model: "opus", Status: store.Running, Pid: 1},
		{Model: "gpt", Status: store.Waiting, Pid: 2},
		{Model: "opus", Status: store.Idle, Pid: 3},
	}}

	// One window spanning a running and a waiting agent pid -> waiting wins.
	model, status := e.enrich(pidSet{1: true, 2: true})
	if status != store.Waiting {
		t.Errorf("status = %q, want waiting", status)
	}
	if model != "opus/gpt" {
		t.Errorf("model = %q, want \"opus/gpt\"", model)
	}
}

func TestEnrichDedupesAndIgnoresUnknownPIDs(t *testing.T) {
	e := &LiveEnricher{live: []store.Status{
		{Model: "opus", Status: store.Running, Pid: 1},
		{Model: "opus", Status: store.Idle, Pid: 2}, // same model, second pid
	}}
	// Both matching pids + an unknown pid -> deduped model, unknown ignored,
	// most-urgent status wins.
	model, status := e.enrich(pidSet{1: true, 2: true, 99: true})
	if model != "opus" || status != store.Running {
		t.Fatalf("got %q/%q", model, status)
	}
}

func TestEnrichNoMatchingPIDs(t *testing.T) {
	e := &LiveEnricher{live: []store.Status{
		{Model: "opus", Status: store.Running, Pid: 1},
	}}
	model, status := e.enrich(pidSet{7: true, 8: true})
	if model != "" || status != "" {
		t.Fatalf("expected empty enrichment for no matching pids, got %q/%q", model, status)
	}
}

func TestMoreUrgent(t *testing.T) {
	if moreUrgent(store.Running, store.Waiting) != store.Waiting {
		t.Error("waiting should beat running")
	}
	if moreUrgent(store.Waiting, store.Idle) != store.Waiting {
		t.Error("waiting should beat idle")
	}
	if moreUrgent(store.Idle, "") != store.Idle {
		t.Error("idle should beat empty")
	}
}
