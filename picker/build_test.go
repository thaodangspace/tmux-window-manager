package picker

import (
	"strconv"
	"strings"
	"testing"

	"github.com/dtonair/tmux-window-manager/store"
	"github.com/dtonair/tmux-window-manager/tmuxcli"
)

// stubEnricher lets tests inject badges without a live tmux/agent.
type stubEnricher struct {
	windows map[string]WindowBadge // key "session:index"
}

func (s stubEnricher) Window(session string, index int, _, _ string) WindowBadge {
	return s.windows[session+":"+strconv.Itoa(index)]
}

// buildRows mirrors Build but takes an explicit window slice so we don't need a
// running tmux server in the test.
func buildRows(windows []tmuxcli.Window, e Enricher) string {
	if e == nil {
		e = NoopEnricher{}
	}
	var b strings.Builder
	prev := ""
	for _, w := range windows {
		if w.Session != prev {
			writeHeader(&b, w.Session)
			prev = w.Session
		}
		writeWindow(&b, w, e.Window(w.Session, w.Index, w.Name, w.Command))
	}
	return b.String()
}

func TestBuildPlainWindows(t *testing.T) {
	windows := []tmuxcli.Window{
		{Session: "work", Index: 1, Active: true, Name: "editor", Command: "nvim"},
		{Session: "work", Index: 2, Active: false, Name: "shell", Command: "zsh"},
		{Session: "logs", Index: 1, Active: false, Name: "tail", Command: "less"},
	}
	out := buildRows(windows, NoopEnricher{})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	// Expect: header(work), 2 windows, header(logs), 1 window = 5 rows.
	if len(lines) != 5 {
		t.Fatalf("got %d rows, want 5:\n%s", len(lines), out)
	}

	targets := make([]string, len(lines))
	for i, ln := range lines {
		targets[i] = strings.SplitN(ln, "\t", 2)[0]
	}
	want := []string{"work", "work:1", "work:2", "logs", "logs:1"}
	for i := range want {
		if targets[i] != want[i] {
			t.Errorf("row %d target = %q, want %q", i, targets[i], want[i])
		}
	}

	// Active window 1 gets the green dot; idle window 2 does not.
	if !strings.Contains(lines[1], Green+"●"+Rst) {
		t.Errorf("active window row missing green dot: %q", lines[1])
	}
	if strings.Contains(lines[2], "●") {
		t.Errorf("idle window row should not have a dot: %q", lines[2])
	}
	// Idle window shows "(cmd)" dim.
	if !strings.Contains(lines[1], "(nvim)") {
		t.Errorf("window row missing command: %q", lines[1])
	}
	// With no pane path, the dimmed row prefix falls back to "session:index".
	if !strings.Contains(lines[1], Dim+"work:1"+Rst) {
		t.Errorf("window row missing dim session:index fallback: %q", lines[1])
	}
}

// TestBuildHeaderAndRowPrefix asserts the header stays the session name (the
// target for switching), while each window row's dimmed prefix shows that
// window's own current-directory basename — even when windows in one session sit
// in different directories. The row target stays "session:index".
func TestBuildHeaderAndRowPrefix(t *testing.T) {
	windows := []tmuxcli.Window{
		{Session: "cli", Index: 1, Active: true, Name: "editor", Command: "nvim", Path: "/Users/dt/code/tmux-window-manager"},
		{Session: "cli", Index: 3, Active: false, Name: "shell", Command: "zsh", Path: "/Users/dt/code/chatgpt-cli"},
	}
	out := buildRows(windows, NoopEnricher{})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	// Header: target and display are both the session name.
	target, display, _ := strings.Cut(lines[0], "\t")
	if target != "cli" || !strings.Contains(display, "cli") {
		t.Errorf("header = %q/%q, want session name \"cli\"", target, display)
	}

	// Row 1: target is cli:1, but the dimmed prefix shows its directory basename.
	if tgt := strings.SplitN(lines[1], "\t", 2)[0]; tgt != "cli:1" {
		t.Errorf("row 1 target = %q, want \"cli:1\"", tgt)
	}
	if !strings.Contains(lines[1], Dim+"tmux-window-manager:1"+Rst) {
		t.Errorf("row 1 missing dir prefix \"tmux-window-manager:1\": %q", lines[1])
	}

	// Row 2 is in a different directory within the same session, so its prefix
	// reflects that directory, not the session or the first window's directory.
	if tgt := strings.SplitN(lines[2], "\t", 2)[0]; tgt != "cli:3" {
		t.Errorf("row 2 target = %q, want \"cli:3\"", tgt)
	}
	if !strings.Contains(lines[2], Dim+"chatgpt-cli:3"+Rst) {
		t.Errorf("row 2 missing dir prefix \"chatgpt-cli:3\": %q", lines[2])
	}
}

func TestBuildAgentBadges(t *testing.T) {
	windows := []tmuxcli.Window{
		{Session: "ai", Index: 1, Active: true, Name: "claude", Command: "node"},
	}
	e := stubEnricher{
		windows: map[string]WindowBadge{"ai:1": {AgentLabel: "claude(opus)", Status: store.Running}},
	}
	out := buildRows(windows, e)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	// Session header is plain: just the session name, no agent badge or icon.
	if strings.Contains(lines[0], Robot) || strings.Contains(lines[0], "claude(opus)") {
		t.Errorf("header should be plain, got: %q", lines[0])
	}
	// Window row shows agent label + robot + status text.
	if !strings.Contains(lines[1], "claude(opus)") {
		t.Errorf("window row missing agent name/model: %q", lines[1])
	}
	if !strings.Contains(lines[1], Robot) || !strings.Contains(lines[1], Italic+"running") {
		t.Errorf("window row missing robot/running text: %q", lines[1])
	}
}

// TestBuildStatusText asserts each status renders its distinct trailing
// italicized label on the window row where the agent runs (the session header
// is always plain).
func TestBuildStatusText(t *testing.T) {
	windows := []tmuxcli.Window{
		{Session: "run", Index: 1, Name: "a", Command: "node"},
		{Session: "wait", Index: 1, Name: "b", Command: "node"},
		{Session: "idle", Index: 1, Name: "c", Command: "node"},
	}
	e := stubEnricher{
		windows: map[string]WindowBadge{
			"run:1":  {AgentLabel: "claude", Status: store.Running},
			"wait:1": {AgentLabel: "claude", Status: store.Waiting},
			"idle:1": {AgentLabel: "claude", Status: store.Idle},
		},
	}
	out := buildRows(windows, e)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// lines: 0 run header, 1 window, 2 wait header, 3 window, 4 idle header, 5 window
	cases := []struct {
		line                     int
		wantRunning, wantWaiting bool
	}{
		{1, true, false},  // running window -> "running" text, no "waiting"
		{3, false, true},  // waiting window -> "waiting" text, no "running"
		{5, false, false}, // idle window -> robot only
	}
	for _, c := range cases {
		ln := lines[c.line]
		if !strings.Contains(ln, Robot) {
			t.Errorf("line %d missing robot: %q", c.line, ln)
		}
		if got := strings.Contains(ln, Italic+"running"); got != c.wantRunning {
			t.Errorf("line %d running=%v want %v: %q", c.line, got, c.wantRunning, ln)
		}
		if got := strings.Contains(ln, Italic+"waiting"); got != c.wantWaiting {
			t.Errorf("line %d waiting=%v want %v: %q", c.line, got, c.wantWaiting, ln)
		}
	}
}
