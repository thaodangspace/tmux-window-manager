package sidebar

import (
	"reflect"
	"testing"

	"github.com/thaodangspace/tmux-window-manager/agents"
	"github.com/thaodangspace/tmux-window-manager/store"
)

// identityRoot returns a GitRoot stub that treats each pane path as its own
// workspace key (no filesystem walk), so grouping/label logic is testable.
func identityRoot(path string) string { return path }

// mapRoot returns a GitRoot stub backed by an explicit path->root map; an
// unmapped path yields "" (Collect then falls back to the path).
func mapRoot(m map[string]string) func(string) string {
	return func(p string) string { return m[p] }
}

func TestCollectNestedChainSingleRow(t *testing.T) {
	det := agents.NewDetectorFromSnapshot("100 1 zsh\n110 100 claude\n120 110 codex\n")
	in := Input{
		Panes:   []PaneInfo{{Session: "s", WindowIndex: 1, WindowID: "@1", PaneID: "%1", PID: "100", Path: "/proj"}},
		Det:     det,
		GitRoot: identityRoot,
	}
	snap := Collect(in)

	rows := allRows(snap)
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d: %+v", len(rows), rows)
	}
	if rows[0].ID != "claude" {
		t.Fatalf("want outermost claude, got %q", rows[0].ID)
	}
	if rows[0].Display != "Claude Code" {
		t.Fatalf("want display Claude Code, got %q", rows[0].Display)
	}
}

func TestCollectSharedPidNewestWins(t *testing.T) {
	det := agents.NewDetectorFromSnapshot("110 1 claude\n")
	in := Input{
		Panes: []PaneInfo{{Session: "s", WindowIndex: 1, PaneID: "%1", PID: "110", Path: "/proj"}},
		Det:   det,
		Live: []store.Status{
			{Pid: 110, Status: store.Running, UpdatedAt: 100},
			{Pid: 110, Status: store.Waiting, UpdatedAt: 200, Detail: "newest"},
		},
		GitRoot: identityRoot,
	}
	rows := allRows(Collect(in))
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].Status != store.Waiting {
		t.Fatalf("want newest status waiting, got %q", rows[0].Status)
	}
	if !rows[0].Hooked {
		t.Fatal("want Hooked=true for matched row")
	}
}

func TestCollectHooklessPi(t *testing.T) {
	det := agents.NewDetectorFromSnapshot("210 1 pi\n")
	in := Input{
		Panes:   []PaneInfo{{Session: "s", WindowIndex: 1, PaneID: "%1", PID: "210", Path: "/proj"}},
		Det:     det,
		Live:    []store.Status{{Pid: 999, Status: store.Running}}, // no match
		GitRoot: identityRoot,
	}
	rows := allRows(Collect(in))
	if len(rows) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows))
	}
	if rows[0].Hooked {
		t.Fatal("want Hooked=false for pi without a DB row")
	}
	if rows[0].Status != store.Idle {
		t.Fatalf("want idle, got %q", rows[0].Status)
	}
}

func TestCollectOrphanDBRowOmitted(t *testing.T) {
	det := agents.NewDetectorFromSnapshot("110 1 claude\n")
	in := Input{
		Panes:   []PaneInfo{{Session: "s", WindowIndex: 1, PaneID: "%1", PID: "110", Path: "/proj"}},
		Det:     det,
		Live:    []store.Status{{Pid: 555, Status: store.Waiting}}, // orphan
		GitRoot: identityRoot,
	}
	rows := allRows(Collect(in))
	if len(rows) != 1 {
		t.Fatalf("orphan DB row must not create a row; got %d rows", len(rows))
	}
	if rows[0].PID == 555 {
		t.Fatal("orphan pid leaked into a row")
	}
}

func TestCollectSidebarPanesSkipped(t *testing.T) {
	det := agents.NewDetectorFromSnapshot("110 1 claude\n210 1 codex\n")
	in := Input{
		Panes: []PaneInfo{
			{Session: "s", WindowIndex: 1, PaneID: "%1", PID: "110", Path: "/proj", Sidebar: true},
			{Session: "s", WindowIndex: 1, PaneID: "%2", PID: "210", Path: "/proj"},
		},
		Det:     det,
		GitRoot: identityRoot,
	}
	rows := allRows(Collect(in))
	if len(rows) != 1 {
		t.Fatalf("want only the non-sidebar pane's agent, got %d", len(rows))
	}
	if rows[0].ID != "codex" {
		t.Fatalf("want codex, got %q", rows[0].ID)
	}
}

func TestCollectBasenameCollision(t *testing.T) {
	det := agents.NewDetectorFromSnapshot("110 1 claude\n210 1 claude\n")
	in := Input{
		Panes: []PaneInfo{
			{Session: "s", WindowIndex: 1, PaneID: "%1", PID: "110", Path: "/home/u/a/proj"},
			{Session: "s", WindowIndex: 2, PaneID: "%2", PID: "210", Path: "/home/u/b/proj"},
		},
		Det:     det,
		Home:    "/home/u",
		GitRoot: identityRoot,
	}
	snap := Collect(in)
	if len(snap.Workspaces) != 2 {
		t.Fatalf("want 2 workspaces, got %d", len(snap.Workspaces))
	}
	// Sorted by label: "proj (a)" before "proj (b)".
	if snap.Workspaces[0].Label != "proj (a)" || snap.Workspaces[1].Label != "proj (b)" {
		t.Fatalf("want disambiguated labels, got %q and %q",
			snap.Workspaces[0].Label, snap.Workspaces[1].Label)
	}
}

func TestCollectHomeLabel(t *testing.T) {
	det := agents.NewDetectorFromSnapshot("110 1 claude\n")
	in := Input{
		Panes:   []PaneInfo{{Session: "s", WindowIndex: 1, PaneID: "%1", PID: "110", Path: "/home/u"}},
		Det:     det,
		Home:    "/home/u",
		GitRoot: mapRoot(nil), // GitRoot "" -> key falls back to cwd == home
	}
	snap := Collect(in)
	if len(snap.Workspaces) != 1 {
		t.Fatalf("want 1 workspace, got %d", len(snap.Workspaces))
	}
	if snap.Workspaces[0].Label != "~" {
		t.Fatalf("want ~ for home, got %q", snap.Workspaces[0].Label)
	}
}

func TestCollectShuffledInputStableOrder(t *testing.T) {
	det := agents.NewDetectorFromSnapshot("110 1 claude\n210 1 codex\n310 1 pi\n")
	panesA := []PaneInfo{
		{Session: "a", WindowIndex: 1, PaneID: "%1", PID: "110", Path: "/x"},
		{Session: "b", WindowIndex: 2, PaneID: "%2", PID: "210", Path: "/y"},
		{Session: "c", WindowIndex: 3, PaneID: "%3", PID: "310", Path: "/z"},
	}
	panesB := []PaneInfo{panesA[2], panesA[0], panesA[1]}

	snapA := Collect(Input{Panes: panesA, Det: det, GitRoot: identityRoot})
	snapB := Collect(Input{Panes: panesB, Det: det, GitRoot: identityRoot})

	if !reflect.DeepEqual(snapA, snapB) {
		t.Fatalf("shuffled input produced different snapshots:\n%+v\n%+v", snapA, snapB)
	}
}

func TestCollectSubtitleChain(t *testing.T) {
	det := agents.NewDetectorFromSnapshot("110 1 claude\n")
	base := func(live []store.Status) Row {
		in := Input{
			Panes:   []PaneInfo{{Session: "s", WindowIndex: 4, PaneID: "%1", PID: "110", Path: "/proj"}},
			Det:     det,
			Live:    live,
			GitRoot: identityRoot,
		}
		return allRows(Collect(in))[0]
	}

	// waiting Detail wins.
	if got := base([]store.Status{{Pid: 110, Status: store.Waiting, Detail: "needs permission", TurnPrompt: "ignored"}}).Subtitle; got != "needs permission" {
		t.Fatalf("waiting detail: want %q, got %q", "needs permission", got)
	}
	// else first line of a multi-line TurnPrompt.
	if got := base([]store.Status{{Pid: 110, Status: store.Running, TurnPrompt: "first line\nsecond line"}}).Subtitle; got != "first line" {
		t.Fatalf("turn prompt: want %q, got %q", "first line", got)
	}
	// else session:index.
	if got := base([]store.Status{{Pid: 110, Status: store.Running}}).Subtitle; got != "s:4" {
		t.Fatalf("fallback: want %q, got %q", "s:4", got)
	}
}

func TestCollectCurrentFlag(t *testing.T) {
	det := agents.NewDetectorFromSnapshot("110 1 claude\n210 1 codex\n")
	in := Input{
		Panes: []PaneInfo{
			{Session: "s", WindowIndex: 1, WindowID: "@7", PaneID: "%1", PID: "110", Path: "/x"},
			{Session: "s", WindowIndex: 2, WindowID: "@8", PaneID: "%2", PID: "210", Path: "/x"},
		},
		Det:          det,
		GitRoot:      identityRoot,
		SelfWindowID: "@7",
	}
	rows := allRows(Collect(in))
	var current, other Row
	for _, r := range rows {
		if r.ID == "claude" {
			current = r
		} else {
			other = r
		}
	}
	if !current.Current {
		t.Fatal("want Current=true for the sidebar's own window")
	}
	if other.Current {
		t.Fatal("want Current=false for other window")
	}
}

func TestCollectNilLiveAllHookless(t *testing.T) {
	det := agents.NewDetectorFromSnapshot("110 1 claude\n210 1 codex\n")
	in := Input{
		Panes: []PaneInfo{
			{Session: "s", WindowIndex: 1, PaneID: "%1", PID: "110", Path: "/x"},
			{Session: "s", WindowIndex: 2, PaneID: "%2", PID: "210", Path: "/x"},
		},
		Det:     det,
		Live:    nil,
		GitRoot: identityRoot,
	}
	for _, r := range allRows(Collect(in)) {
		if r.Hooked {
			t.Fatalf("nil Live: want all hookless, %q was hooked", r.ID)
		}
		if r.Status != store.Idle {
			t.Fatalf("nil Live: want idle, got %q", r.Status)
		}
	}
}

// TestCollectNumericWindowIndexSort pins the row sort to a numeric window index,
// not a lexical one: window 2 must precede window 10 within a workspace. A string
// compare of "2" vs "10" would invert them.
func TestCollectNumericWindowIndexSort(t *testing.T) {
	det := agents.NewDetectorFromSnapshot("110 1 claude\n210 1 codex\n")
	in := Input{
		Panes: []PaneInfo{
			{Session: "s", WindowIndex: 10, PaneID: "%1", PID: "110", Path: "/proj"},
			{Session: "s", WindowIndex: 2, PaneID: "%2", PID: "210", Path: "/proj"},
		},
		Det:     det,
		GitRoot: identityRoot,
	}
	rows := allRows(Collect(in))
	if len(rows) != 2 {
		t.Fatalf("want 2 rows, got %d", len(rows))
	}
	if rows[0].Target != "s:2" || rows[1].Target != "s:10" {
		t.Fatalf("want numeric order s:2 before s:10, got %q then %q",
			rows[0].Target, rows[1].Target)
	}
}

// TestCollectInnerPidDBRowJoins encodes FR5: a live DB row is joined to an agent
// row when its pid is the outer agent's pid OR any agent descendant of it. The
// hook handler resolves an event to the *nearest* agent (NearestAgent), so a
// nested codex launched by claude records its status under codex's inner pid. The
// panel row is created for the outer claude, so that inner-pid DB row must still
// attach to it; otherwise the status is lost and the row is wrongly dropped as an
// orphan even though it belongs to a real pane.
func TestCollectInnerPidDBRowJoins(t *testing.T) {
	det := agents.NewDetectorFromSnapshot("100 1 zsh\n110 100 claude\n120 110 codex\n")
	in := Input{
		Panes:   []PaneInfo{{Session: "s", WindowIndex: 1, PaneID: "%1", PID: "100", Path: "/proj"}},
		Det:     det,
		Live:    []store.Status{{Pid: 120, Status: store.Waiting, UpdatedAt: 200, Detail: "inner codex waiting"}},
		GitRoot: identityRoot,
	}
	rows := allRows(Collect(in))
	if len(rows) != 1 {
		t.Fatalf("want 1 outer row, got %d: %+v", len(rows), rows)
	}
	if !rows[0].Hooked {
		t.Fatal("outer row must join the inner-pid DB row (Hooked=true)")
	}
	if rows[0].Status != store.Waiting {
		t.Fatalf("want inner status waiting on the outer row, got %q", rows[0].Status)
	}
	if rows[0].Subtitle != "inner codex waiting" {
		t.Fatalf("want inner waiting detail as subtitle, got %q", rows[0].Subtitle)
	}
}

// allRows flattens a snapshot's rows across workspaces, preserving order.
func allRows(s Snapshot) []Row {
	var out []Row
	for _, w := range s.Workspaces {
		out = append(out, w.Agents...)
	}
	return out
}
