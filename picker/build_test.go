package picker

import (
	"strconv"
	"strings"
	"testing"

	"github.com/thaodangspace/tmux-window-manager/store"
	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
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
	return buildRowsFiltered(windows, e, "")
}

func buildRowsFiltered(windows []tmuxcli.Window, e Enricher, query string) string {
	if e == nil {
		e = NoopEnricher{}
	}
	var b strings.Builder
	prev := ""
	var group []windowRow
	flush := func(session string) {
		if session == "" {
			return
		}
		rows := filterGroup(session, group, query)
		if len(rows) == 0 {
			return
		}
		writeHeader(&b, session, groupSearch(rows))
		for _, r := range rows {
			writeWindowRow(&b, r)
		}
	}
	for _, w := range windows {
		if w.Session != prev {
			flush(prev)
			group = group[:0]
			prev = w.Session
		}
		group = append(group, newWindowRow(w, e.Window(w.Session, w.Index, w.Name, w.Command)))
	}
	flush(prev)
	return b.String()
}

func displayField(line string) string {
	parts := strings.Split(line, "\t")
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

func searchField(line string) string {
	parts := strings.Split(line, "\t")
	if len(parts) < 3 {
		return ""
	}
	return parts[2]
}

func TestBuildPlainWindows(t *testing.T) {
	windows := []tmuxcli.Window{
		{Session: "work", Index: 1, Active: true, Name: "editor", Command: "nvim", Path: "/Users/dt/code/app"},
		{Session: "work", Index: 2, Active: false, Name: "shell", Command: "zsh", Path: "/Users/dt/code/api"},
		{Session: "logs", Index: 1, Active: false, Name: "tail", Command: "less", Path: "/var/log"},
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
	// Window rows mirror the tmux status-panel label: basename(cwd)/label.
	display := displayField(lines[1])
	if !strings.Contains(display, "app/nvim") {
		t.Errorf("window row missing status-panel label: %q", lines[1])
	}
	if strings.Contains(display, "work:1") || strings.Contains(display, "editor") {
		t.Errorf("window row display should not include target or raw window_name: %q", lines[1])
	}
	// Hidden search still carries command, raw window name, path, and target for filtering/switching.
	if got := searchField(lines[1]); !strings.Contains(got, "work:1") || !strings.Contains(got, "nvim") || !strings.Contains(got, "editor") || !strings.Contains(got, "/Users/dt/code/app") {
		t.Errorf("window row hidden search should include target, command, window name, and path: %q", lines[1])
	}
}

// TestBuildHeaderAndRowPrefix asserts the header stays the session name and
// window rows mirror tmux's status-panel label while keeping raw window names
// and session:index targets out of the visible display. The hidden row target
// still stays "session:index".
func TestBuildWindowDisplayShowsStatusPanelLabelWithoutWindowContext(t *testing.T) {
	windows := []tmuxcli.Window{
		{Session: "cli", Index: 1, Active: true, Name: "editor", Command: "nvim", Path: "/Users/dt/code/tmux-window-manager"},
		{Session: "cli", Index: 3, Active: false, Name: "shell", Command: "zsh", Path: "/Users/dt/code/chatgpt-cli"},
	}
	out := buildRows(windows, NoopEnricher{})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	// Header: target and display are both the session name.
	target := strings.SplitN(lines[0], "\t", 2)[0]
	display := displayField(lines[0])
	if target != "cli" || !strings.Contains(display, "cli") {
		t.Errorf("header = %q/%q, want session name \"cli\"", target, display)
	}

	// Row 1: target is cli:1. Display shows basename(cwd)/label and never
	// includes the full cwd, raw window name, or visible session:index context.
	if tgt := strings.SplitN(lines[1], "\t", 2)[0]; tgt != "cli:1" {
		t.Errorf("row 1 target = %q, want \"cli:1\"", tgt)
	}
	display1 := displayField(lines[1])
	if strings.Contains(display1, "/Users/dt/code") {
		t.Errorf("row 1 visible display should not include full current dir: %q", lines[1])
	}
	if !strings.Contains(display1, "tmux-window-manager/nvim") {
		t.Errorf("row 1 display should include status-panel label: %q", lines[1])
	}
	if strings.Contains(display1, "editor") || strings.Contains(display1, "cli:1") || strings.Contains(display1, "/Users/dt/code") {
		t.Errorf("row 1 display should not include raw window name, visible window context, or full cwd: %q", lines[1])
	}
	if got := searchField(lines[1]); !strings.Contains(got, "cli:1") || !strings.Contains(got, "nvim") || !strings.Contains(got, "editor") {
		t.Errorf("row 1 hidden search should include window context, command, and raw window name: %q", lines[1])
	}

	// Row 2 is in a different directory within the same session, but display
	// still shows the same status-panel label shape, never full cwd, raw window
	// name, or visible session:index.
	if tgt := strings.SplitN(lines[2], "\t", 2)[0]; tgt != "cli:3" {
		t.Errorf("row 2 target = %q, want \"cli:3\"", tgt)
	}
	display2 := displayField(lines[2])
	if strings.Contains(display2, "/Users/dt/code") {
		t.Errorf("row 2 visible display should not include full current dir: %q", lines[2])
	}
	if !strings.Contains(display2, "chatgpt-cli/zsh") {
		t.Errorf("row 2 display should include status-panel label: %q", lines[2])
	}
	if strings.Contains(display2, "shell") || strings.Contains(display2, "cli:3") || strings.Contains(display2, "/Users/dt/code") {
		t.Errorf("row 2 display should not include raw window name, visible window context, or full cwd: %q", lines[2])
	}
	if got := searchField(lines[2]); !strings.Contains(got, "cli:3") || !strings.Contains(got, "zsh") || !strings.Contains(got, "shell") {
		t.Errorf("row 2 hidden search should include window context, command, and raw window name: %q", lines[2])
	}
}

func TestBuildHeaderSearchIncludesChildWindowTerms(t *testing.T) {
	windows := []tmuxcli.Window{
		{Session: "vc-api-emr", Index: 1, Name: "claude", Command: "node"},
		{Session: "vc-page-admin", Index: 1, Name: "node", Command: "node"},
		{Session: "vc-page-admin", Index: 2, Name: "claude", Command: "node"},
	}
	e := stubEnricher{
		windows: map[string]WindowBadge{
			"vc-api-emr:1":    {AgentLabel: "claude(claude-opus-4-8)", Status: store.Waiting},
			"vc-page-admin:2": {AgentLabel: "claude(claude-opus-4-8)", Status: store.Running},
		},
	}
	out := buildRows(windows, e)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	if got := displayField(lines[0]); !strings.Contains(got, "vc-api-emr") || strings.Contains(got, "claude") {
		t.Errorf("header display should stay group-only, got %q", got)
	}
	if got := searchField(lines[0]); !strings.Contains(got, "claude") || !strings.Contains(got, "vc-api-emr:1") {
		t.Errorf("header search should include child claude row terms, got %q", got)
	}

	if got := displayField(lines[2]); !strings.Contains(got, "vc-page-admin") || strings.Contains(got, "claude") {
		t.Errorf("second header display should stay group-only, got %q", got)
	}
	if got := searchField(lines[2]); !strings.Contains(got, "claude") || !strings.Contains(got, "vc-page-admin:2") {
		t.Errorf("second header search should include child claude row terms, got %q", got)
	}
}

func TestBuildFilteredPreservesMatchingGroups(t *testing.T) {
	windows := []tmuxcli.Window{
		{Session: "vc-api-emr", Index: 1, Name: "claude", Command: "node"},
		{Session: "vc-page-admin", Index: 1, Name: "node", Command: "node"},
		{Session: "vc-page-admin", Index: 2, Name: "claude", Command: "node"},
		{Session: "vc-page-emr", Index: 1, Name: "node", Command: "node"},
	}
	e := stubEnricher{
		windows: map[string]WindowBadge{
			"vc-api-emr:1":    {AgentLabel: "claude(claude-opus-4-8)", Status: store.Waiting},
			"vc-page-admin:2": {AgentLabel: "claude(claude-opus-4-8)", Status: store.Running},
		},
	}
	out := buildRowsFiltered(windows, e, "claude")
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	targets := make([]string, len(lines))
	for i, line := range lines {
		targets[i] = strings.SplitN(line, "\t", 2)[0]
	}
	want := []string{"vc-api-emr", "vc-api-emr:1", "vc-page-admin", "vc-page-admin:2"}
	if strings.Join(targets, "\n") != strings.Join(want, "\n") {
		t.Fatalf("filtered targets = %#v, want %#v\n%s", targets, want, out)
	}
	if strings.Contains(out, "vc-page-admin:1") || strings.Contains(out, "vc-page-emr") {
		t.Fatalf("filtered output should omit non-matching child rows/groups:\n%s", out)
	}
}

func TestBuildAgentBadges(t *testing.T) {
	windows := []tmuxcli.Window{
		{Session: "ai", Index: 1, Active: true, Name: "node", Command: "node", Path: "/Users/dt/code/tmux-window-manager"},
	}
	e := stubEnricher{
		windows: map[string]WindowBadge{"ai:1": {AgentLabel: "claude(opus)", PaneLabel: "claude", Status: store.Running}},
	}
	out := buildRows(windows, e)
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")

	// Session header is plain: just the session name, no agent badge or icon.
	if got := displayField(lines[0]); strings.Contains(got, Robot) || strings.Contains(got, "claude(opus)") {
		t.Errorf("header should be plain, got: %q", lines[0])
	}
	// Window row shows the status-panel label, robot, and status text; the custom
	// agent/model label remains hidden search metadata only.
	display := displayField(lines[1])
	if !strings.Contains(display, "tmux-window-manager/claude") {
		t.Errorf("window row missing status-panel label: %q", lines[1])
	}
	if strings.Contains(display, "claude(opus)") || strings.Contains(display, "ai:1") || strings.Contains(display, "node") {
		t.Errorf("window row display should not include agent/model, target, or raw command/window name: %q", lines[1])
	}
	if !strings.Contains(lines[1], Robot) || !strings.Contains(lines[1], Italic+"running") {
		t.Errorf("window row missing robot/running text: %q", lines[1])
	}
	if got := searchField(lines[1]); !strings.Contains(got, "claude(opus)") || !strings.Contains(got, "ai:1") || !strings.Contains(got, "node") {
		t.Errorf("window row hidden search should include agent/model, target, and command: %q", lines[1])
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
