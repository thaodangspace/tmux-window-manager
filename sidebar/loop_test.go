package sidebar

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/thaodangspace/tmux-window-manager/store"
	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
)

// selfID is the sidebar pane's own pane id used across the loop tests.
const selfID = "%0"

// psSnap is a process snapshot with one shell pane (pid 1000) hosting a claude
// agent (pid 2000), so Collect yields exactly one row.
const psSnap = "1000 1 zsh\n2000 1000 claude\n"

// testPanes returns a self sidebar pane plus one real agent pane, both in an
// active, attached, non-zoomed window (so the sidebar is visible).
func testPanes() []tmuxcli.SidebarPane {
	return []tmuxcli.SidebarPane{
		{
			Session: "work", WindowIndex: 1, WindowID: "@1", PaneID: selfID,
			PanePID: "999", StartCommand: "'twm' sidebar render",
			WindowActive: true, SessionAttached: true, Sidebar: true,
		},
		{
			Session: "work", WindowIndex: 1, WindowID: "@1", PaneID: "%1",
			PanePID: "1000", Path: "/home/u/proj",
			WindowActive: true, SessionAttached: true,
		},
	}
}

// fakeDB is an injectable store surface counting its calls.
type fakeDB struct {
	rows      []store.Status
	liveErr   error
	dv        int64
	liveCalls int
	dvCalls   int
}

func (f *fakeDB) Live() ([]store.Status, error) {
	f.liveCalls++
	return f.rows, f.liveErr
}

func (f *fakeDB) DataVersion() (int64, error) {
	f.dvCalls++
	return f.dv, nil
}

type fakeTicker struct{ ch chan time.Time }

func (f *fakeTicker) C() <-chan time.Time   { return f.ch }
func (f *fakeTicker) Reset(d time.Duration) {}
func (f *fakeTicker) Stop()                 {}

// baseDeps wires a visible sidebar with a live claude row and counts ps calls.
func baseDeps(out *bytes.Buffer, db DB, psCalls *int) Deps {
	panes := testPanes()
	return Deps{
		Self:      selfID,
		Home:      "/home/u",
		GitRoot:   func(string) string { return "/home/u/proj" },
		ListPanes: func() ([]tmuxcli.SidebarPane, error) { return panes, nil },
		Snapshot: func() string {
			if psCalls != nil {
				*psCalls++
			}
			return psSnap
		},
		DB:      db,
		Size:    func() (int, int) { return 32, 20 },
		Out:     out,
		Refresh: 10 * time.Millisecond,
	}
}

func frameCount(b []byte) int {
	return bytes.Count(b, []byte(cursorHome))
}

func TestVisible(t *testing.T) {
	base := func(mut func(*tmuxcli.SidebarPane)) []tmuxcli.SidebarPane {
		p := tmuxcli.SidebarPane{
			PaneID: selfID, WindowActive: true, SessionAttached: true,
		}
		mut(&p)
		return []tmuxcli.SidebarPane{p}
	}
	cases := []struct {
		name  string
		panes []tmuxcli.SidebarPane
		want  bool
	}{
		{"visible", base(func(*tmuxcli.SidebarPane) {}), true},
		{"grouped-session attached", base(func(p *tmuxcli.SidebarPane) { p.SessionAttached = true }), true},
		{"inactive window", base(func(p *tmuxcli.SidebarPane) { p.WindowActive = false }), false},
		{"detached", base(func(p *tmuxcli.SidebarPane) { p.SessionAttached = false }), false},
		{"zoomed", base(func(p *tmuxcli.SidebarPane) { p.WindowZoomed = true }), false},
		{"off", base(func(p *tmuxcli.SidebarPane) { p.SidebarOff = "1" }), false},
		{"disabled", base(func(p *tmuxcli.SidebarPane) { p.SidebarEnabled = "0" }), false},
		{"self missing", []tmuxcli.SidebarPane{{PaneID: "%9", WindowActive: true, SessionAttached: true}}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Visible(c.panes, selfID); got != c.want {
				t.Fatalf("Visible = %v, want %v", got, c.want)
			}
		})
	}
}

func TestTickRendersOnce(t *testing.T) {
	var out bytes.Buffer
	db := &fakeDB{rows: []store.Status{{Agent: "claude", Pid: 2000, Status: store.Running, UpdatedAt: 1}}, dv: 1}
	l := newLooper(baseDeps(&out, db, nil))

	visible, exit := l.tick()
	if !visible || exit {
		t.Fatalf("first tick visible=%v exit=%v, want true/false", visible, exit)
	}
	if frameCount(out.Bytes()) != 1 {
		t.Fatalf("want 1 frame, got %d", frameCount(out.Bytes()))
	}
	if !strings.Contains(out.String(), "working") {
		t.Fatalf("frame missing running row: %q", out.String())
	}
}

func TestTickUnchangedInputNoSecondWrite(t *testing.T) {
	var out bytes.Buffer
	db := &fakeDB{rows: []store.Status{{Agent: "claude", Pid: 2000, Status: store.Running, UpdatedAt: 1}}, dv: 1}
	l := newLooper(baseDeps(&out, db, nil))

	l.tick()
	l.tick()
	if n := frameCount(out.Bytes()); n != 1 {
		t.Fatalf("unchanged input wrote %d frames, want 1", n)
	}
}

func TestTickSkipsLiveWhenUnchanged(t *testing.T) {
	var out bytes.Buffer
	db := &fakeDB{rows: []store.Status{{Agent: "claude", Pid: 2000, Status: store.Idle, UpdatedAt: 1}}, dv: 7}
	l := newLooper(baseDeps(&out, db, nil))

	l.tick()
	l.tick()
	l.tick()
	if db.liveCalls != 1 {
		t.Fatalf("Live called %d times, want 1 (data_version + pane set unchanged)", db.liveCalls)
	}
}

func TestTickThrottlesPS(t *testing.T) {
	var out bytes.Buffer
	psCalls := 0
	db := &fakeDB{dv: 1}
	l := newLooper(baseDeps(&out, db, &psCalls))

	l.tick() // det nil -> ps
	l.tick() // even tick -> ps
	l.tick() // odd tick, pane set unchanged -> reuse
	if psCalls != 2 {
		t.Fatalf("ps ran %d times over 3 ticks, want 2 (throttled/reused)", psCalls)
	}
}

func TestTickLiveErrorStillRenders(t *testing.T) {
	var out bytes.Buffer
	db := &fakeDB{liveErr: errors.New("boom"), dv: 1}
	l := newLooper(baseDeps(&out, db, nil))

	visible, exit := l.tick()
	if !visible || exit {
		t.Fatalf("tick visible=%v exit=%v, want true/false", visible, exit)
	}
	if frameCount(out.Bytes()) != 1 {
		t.Fatalf("Live error should still render one frame, got %d", frameCount(out.Bytes()))
	}
}

func TestTickNilDBRenders(t *testing.T) {
	var out bytes.Buffer
	l := newLooper(baseDeps(&out, nil, nil))

	visible, exit := l.tick()
	if !visible || exit {
		t.Fatalf("tick visible=%v exit=%v, want true/false", visible, exit)
	}
	if frameCount(out.Bytes()) != 1 {
		t.Fatalf("nil DB should render one frame, got %d", frameCount(out.Bytes()))
	}
}

func TestTickDisabledExits(t *testing.T) {
	var out bytes.Buffer
	deps := baseDeps(&out, &fakeDB{dv: 1}, nil)
	panes := testPanes()
	panes[0].SidebarEnabled = "0" // runtime disable on self pane
	deps.ListPanes = func() ([]tmuxcli.SidebarPane, error) { return panes, nil }
	l := newLooper(deps)

	_, exit := l.tick()
	if !exit {
		t.Fatal("disabled sidebar should exit the loop")
	}
	if frameCount(out.Bytes()) != 0 {
		t.Fatalf("disabled tick should not render, got %d frames", frameCount(out.Bytes()))
	}
}

func TestTickHiddenNoRender(t *testing.T) {
	var out bytes.Buffer
	psCalls := 0
	deps := baseDeps(&out, &fakeDB{dv: 1}, &psCalls)
	panes := testPanes()
	panes[0].WindowActive = false // hidden
	panes[1].WindowActive = false
	deps.ListPanes = func() ([]tmuxcli.SidebarPane, error) { return panes, nil }
	l := newLooper(deps)

	visible, exit := l.tick()
	if visible || exit {
		t.Fatalf("hidden tick visible=%v exit=%v, want false/false", visible, exit)
	}
	if frameCount(out.Bytes()) != 0 {
		t.Fatalf("hidden tick should not render, got %d frames", frameCount(out.Bytes()))
	}
	if psCalls != 0 {
		t.Fatalf("hidden tick should not run ps, ran %d times", psCalls)
	}
}

// TestLoopHiddenBlocksThenTerminates checks the Loop select path: while hidden it
// does no work and blocks on signals (the ticker never fires here), and a
// terminate signal returns cleanly after restoring the terminal.
func TestLoopHiddenBlocksThenTerminates(t *testing.T) {
	var out bytes.Buffer
	deps := baseDeps(&out, &fakeDB{dv: 1}, nil)
	panes := testPanes()
	panes[0].WindowActive = false
	panes[1].WindowActive = false
	deps.ListPanes = func() ([]tmuxcli.SidebarPane, error) { return panes, nil }

	term := make(chan os.Signal, 1)
	deps.Ticker = &fakeTicker{ch: make(chan time.Time)} // never fires
	deps.Wake = make(chan os.Signal)
	deps.Term = term

	done := make(chan error, 1)
	go func() { done <- Loop(deps) }()

	term <- syscall.SIGTERM
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Loop returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Loop did not return after SIGTERM")
	}

	if frameCount(out.Bytes()) != 0 {
		t.Fatalf("hidden Loop wrote %d frames, want 0", frameCount(out.Bytes()))
	}
	if !strings.Contains(out.String(), enterAltScreen) || !strings.Contains(out.String(), leaveAltScreen) {
		t.Fatal("Loop did not enter/restore the alternate screen")
	}
}

// TestLoopTerminatesOnTermWhenVisible confirms a visible Loop renders and then
// exits cleanly on SIGTERM.
func TestLoopTerminatesOnTermWhenVisible(t *testing.T) {
	var out bytes.Buffer
	deps := baseDeps(&out, &fakeDB{rows: []store.Status{{Agent: "claude", Pid: 2000, Status: store.Running, UpdatedAt: 1}}, dv: 1}, nil)

	term := make(chan os.Signal, 1)
	deps.Ticker = &fakeTicker{ch: make(chan time.Time)}
	deps.Wake = make(chan os.Signal)
	deps.Term = term

	done := make(chan error, 1)
	go func() { done <- Loop(deps) }()

	term <- syscall.SIGTERM
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Loop returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Loop did not return after SIGTERM")
	}
	if frameCount(out.Bytes()) < 1 {
		t.Fatal("visible Loop should have rendered at least one frame")
	}
}
