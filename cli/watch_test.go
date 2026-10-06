package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/thaodangspace/tmux-window-manager/notify"
	"github.com/thaodangspace/tmux-window-manager/store"
	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
)

type fakeNotifier struct{ events []notify.Event }

func (f *fakeNotifier) Notify(_ context.Context, event notify.Event) error {
	f.events = append(f.events, event)
	return nil
}

// fakeWatcher wires a watcher to in-memory panes, screens, verdicts and rows.
type fakeWatcher struct {
	*watcher
	clock    time.Time
	screens  map[string]string
	verdicts map[string]notify.Verdict // keyed by screen
	focus    tmuxcli.PaneFocus
	judged   []string
	rows     map[string]store.Status // keyed by session id
	sender   *fakeNotifier
	pid      int
}

func newFakeWatcher(t *testing.T, opts notify.Options) *fakeWatcher {
	t.Helper()
	f := &fakeWatcher{
		clock:    time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC),
		screens:  map[string]string{},
		verdicts: map[string]notify.Verdict{},
		focus:    tmuxcli.PaneFocus{Location: "work:1"},
		rows:     map[string]store.Status{},
		sender:   &fakeNotifier{},
		pid:      42,
	}
	cfg := notify.PollerConfig{Scan: 5 * time.Second, Settle: 5 * time.Second, Interval: 2 * time.Minute, Lines: 60}
	w := newWatcher(cfg, true, "/tmp/tmux-501/default", nil)
	w.now = func() time.Time { return f.clock }
	w.panes = func() []agentPane {
		var out []agentPane
		for id := range f.screens {
			out = append(out, agentPane{ID: id, Agent: "gemini", PID: f.pid, Cwd: "/work/project"})
		}
		return out
	}
	w.capture = func(pane string) (string, error) { return f.screens[pane], nil }
	w.lookup = func(string) (tmuxcli.PaneFocus, bool) { return f.focus, true }
	w.judge = func(_ context.Context, _, screen string) (notify.Verdict, error) {
		f.judged = append(f.judged, screen)
		return f.verdicts[screen], nil
	}
	w.backends = func() []backend { return []backend{{name: "fake", sender: f.sender, opts: opts}} }
	w.upsert = func(s store.Status) { f.rows[s.SessionID] = s }
	w.remove = func(_, sessionID string) { delete(f.rows, sessionID) }
	f.watcher = w
	return f
}

// scan advances the clock by one scan interval and ticks.
func (f *fakeWatcher) scan() {
	f.clock = f.clock.Add(5 * time.Second)
	f.tick(context.Background())
}

func (f *fakeWatcher) status() string { return f.rows["pane:%1"].Status }

func TestWatcherStatusFollowsScreen(t *testing.T) {
	f := newFakeWatcher(t, notify.Options{})
	f.verdicts["prompt"] = notify.Verdict{State: notify.ScreenIdle, Model: "Gemini 3 Pro"}
	f.verdicts["approve? [y/n]"] = notify.Verdict{State: notify.ScreenWaiting, Summary: "wants to run rm"}

	f.screens["%1"] = "prompt"
	f.tick(context.Background()) // first sight: not settled yet
	if len(f.judged) != 0 || f.status() != "" {
		t.Fatalf("judged=%d status=%q before settling", len(f.judged), f.status())
	}
	f.scan()
	if f.status() != store.Idle || f.rows["pane:%1"].Model != "Gemini 3 Pro" || f.rows["pane:%1"].Pid != 42 {
		t.Fatalf("row after settle = %+v", f.rows["pane:%1"])
	}

	f.screens["%1"] = "thinking 1s"
	f.scan()
	if f.status() != store.Working {
		t.Fatalf("status = %q, want working on a changing screen", f.status())
	}
	f.screens["%1"] = "thinking 9s" // digits only: not a change
	f.scan()
	f.scan()
	if len(f.judged) != 2 || f.judged[1] != "thinking 9s" {
		t.Fatalf("judged %v, want the settled working screen judged once", f.judged)
	}

	f.screens["%1"] = "approve? [y/n]"
	f.scan()
	f.scan()
	f.scan()
	if f.status() != store.Waiting || f.rows["pane:%1"].Model != "Gemini 3 Pro" {
		t.Fatalf("row = %+v, want waiting keeping the model", f.rows["pane:%1"])
	}
	if len(f.sender.events) != 1 || f.sender.events[0].Detail != "wants to run rm" ||
		f.sender.events[0].Pane != "%1" || f.sender.events[0].Location != "work:1" {
		t.Fatalf("events = %+v, want one waiting notification", f.sender.events)
	}

	delete(f.screens, "%1")
	f.scan()
	if _, ok := f.rows["pane:%1"]; ok {
		t.Fatal("row of a closed agent pane was kept")
	}
}

func TestWatcherFollowsAgentRestartInPlace(t *testing.T) {
	f := newFakeWatcher(t, notify.Options{})
	f.verdicts["prompt"] = notify.Verdict{State: notify.ScreenIdle}
	f.screens["%1"] = "prompt"
	f.scan()
	f.scan()
	f.pid = 99
	f.scan()
	if row := f.rows["pane:%1"]; row.Pid != 99 || row.Status != store.Idle {
		t.Fatalf("row = %+v, want idle re-pointed at pid 99", row)
	}
}

func TestWatcherStartupIsSilent(t *testing.T) {
	f := newFakeWatcher(t, notify.Options{})
	f.verdicts["done"] = notify.Verdict{State: notify.ScreenCompleted}
	f.screens["%1"] = "done"
	f.scan()
	f.scan()
	if f.status() != store.Idle || len(f.sender.events) != 0 {
		t.Fatalf("status=%q events=%d, want the startup screen judged but not notified", f.status(), len(f.sender.events))
	}
}

func TestWatcherCompletedTurnDurationAndMinTurn(t *testing.T) {
	f := newFakeWatcher(t, notify.Options{MinTurn: 30 * time.Second, IncludeResponse: true})
	f.verdicts["done short"] = notify.Verdict{State: notify.ScreenCompleted, Summary: "short"}
	f.verdicts["done long"] = notify.Verdict{State: notify.ScreenCompleted, Summary: "tests pass"}
	f.screens["%1"] = "prompt"
	f.scan()

	f.screens["%1"] = "work a"
	f.scan()
	f.screens["%1"] = "done short"
	f.scan()
	f.scan()
	f.scan()
	if len(f.sender.events) != 0 {
		t.Fatalf("a 5s turn notified: %+v", f.sender.events)
	}

	f.screens["%1"] = "work b"
	f.scan()
	for _, s := range []string{"work c", "work d", "work e", "work f", "work g", "work h", "done long"} {
		f.screens["%1"] = s
		f.scan()
	}
	f.scan()
	f.scan()
	if len(f.sender.events) != 1 {
		t.Fatalf("events = %+v, want one completed", f.sender.events)
	}
	e := f.sender.events[0]
	if e.Kind != notify.Completed || e.Duration != 35*time.Second || e.Response != "tests pass" {
		t.Fatalf("completed event = %+v", e)
	}
}

func TestWatcherRejudgesSilentWork(t *testing.T) {
	f := newFakeWatcher(t, notify.Options{})
	f.verdicts["running tests"] = notify.Verdict{State: notify.ScreenWorking}
	f.screens["%1"] = "x"
	f.scan()
	f.screens["%1"] = "running tests"
	f.scan()
	f.scan()
	f.scan()
	n := len(f.judged)
	for i := 0; i < 22; i++ { // under two minutes
		f.scan()
	}
	if len(f.judged) != n {
		t.Fatalf("re-judged before the interval: %d -> %d", n, len(f.judged))
	}
	f.scan()
	if len(f.judged) != n+1 || f.status() != store.Working {
		t.Fatalf("judged=%d status=%q, want one re-judgment after the interval", len(f.judged), f.status())
	}
}

func TestWatcherJudgeFailureBacksOff(t *testing.T) {
	f := newFakeWatcher(t, notify.Options{})
	calls := 0
	f.judge = func(context.Context, string, string) (notify.Verdict, error) {
		calls++
		return notify.Verdict{}, notify.ErrJudge
	}
	f.screens["%1"] = "x"
	f.scan()
	f.screens["%1"] = "y"
	f.scan()
	f.scan()
	f.scan()
	if calls != 1 || f.status() != store.Idle {
		t.Fatalf("calls=%d status=%q, want one attempt and a settled idle", calls, f.status())
	}
	for i := 0; i < 6; i++ {
		f.scan()
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want a retry after the backoff", calls)
	}
}

func TestWatcherWithoutJudge(t *testing.T) {
	f := newFakeWatcher(t, notify.Options{})
	f.judgeOn = false
	f.screens["%1"] = "x"
	f.scan()
	f.screens["%1"] = "y"
	f.scan()
	if f.status() != store.Working {
		t.Fatalf("status = %q, want working", f.status())
	}
	f.scan()
	f.scan()
	if f.status() != store.Idle || len(f.judged) != 0 || len(f.sender.events) != 0 {
		t.Fatalf("status=%q judged=%d events=%d", f.status(), len(f.judged), len(f.sender.events))
	}
}

func TestWatcherSkipsFocusedPane(t *testing.T) {
	f := newFakeWatcher(t, notify.DefaultOptions())
	f.verdicts["ask"] = notify.Verdict{State: notify.ScreenWaiting}
	f.focus.Watched = true
	f.screens["%1"] = "x"
	f.scan()
	f.screens["%1"] = "ask"
	f.scan()
	f.scan()
	f.scan()
	if f.status() != store.Waiting || len(f.sender.events) != 0 {
		t.Fatalf("status=%q events=%d, want waiting recorded but not sent", f.status(), len(f.sender.events))
	}
}

func TestTailLinesAndScreenHash(t *testing.T) {
	if got := tailLines("a\nb  \nc\n\n  \n", 2); got != "b\nc" {
		t.Fatalf("tailLines = %q", got)
	}
	if screenHash("esc to interrupt · 12s") != screenHash("esc to interrupt · 47s") {
		t.Fatal("digit-only change altered the hash")
	}
	if screenHash("working") == screenHash("done") {
		t.Fatal("different screens share a hash")
	}
}

func TestWatchLock(t *testing.T) {
	t.Setenv("TWM_DB_PATH", filepath.Join(t.TempDir(), "agents.db"))
	if runningWatcherPID() != 0 {
		t.Fatal("watcher reported running before the lock was taken")
	}
	lock, err := acquireWatchLock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := acquireWatchLock(); err != errWatcherRunning {
		t.Fatalf("second lock err = %v", err)
	}
	if pid := runningWatcherPID(); pid <= 1 {
		t.Fatalf("runningWatcherPID = %d while locked", pid)
	}
	lock.Close()
	if runningWatcherPID() != 0 {
		t.Fatal("stale lock file treated as a running watcher")
	}
}

func TestWatcherStopsMidJudge(t *testing.T) {
	f := newFakeWatcher(t, notify.Options{})
	ctx, cancel := context.WithCancel(context.Background())
	f.judge = func(ctx context.Context, _, _ string) (notify.Verdict, error) {
		cancel() // the stop signal arrives while the model is thinking
		<-ctx.Done()
		return notify.Verdict{}, ctx.Err()
	}
	f.screens["%1"] = "x"
	f.scan()
	f.screens["%1"] = "y"
	f.scan()
	f.clock = f.clock.Add(5 * time.Second)
	f.tick(ctx)
	if f.status() != store.Working {
		t.Fatalf("status = %q, want the row left untouched when stopping", f.status())
	}
}

func TestWatcherSurvivesTransientServerMiss(t *testing.T) {
	f := newFakeWatcher(t, notify.Options{})
	f.cfg.Scan = time.Millisecond
	// Two misses, one live scan, then the server is gone for good.
	up := []bool{false, false, true}
	calls := 0
	f.serverUp = func() bool {
		calls++
		if calls <= len(up) {
			return up[calls-1]
		}
		return false
	}
	ticks := 0
	f.panes = func() []agentPane { ticks++; return nil }

	done := make(chan struct{})
	go func() {
		f.run(make(chan os.Signal))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watcher never exited after the server went away")
	}
	if ticks != 1 {
		t.Fatalf("ticks = %d, want 1 (the scan while the server was up)", ticks)
	}
	if want := len(up) + serverGoneScans; calls != want {
		t.Fatalf("server checks = %d, want %d", calls, want)
	}
}
