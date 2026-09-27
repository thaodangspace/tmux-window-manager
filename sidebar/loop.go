package sidebar

import (
	"bytes"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/thaodangspace/tmux-window-manager/agents"
	"github.com/thaodangspace/tmux-window-manager/config"
	"github.com/thaodangspace/tmux-window-manager/store"
	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
)

// Terminal control sequences for the render loop's alternate screen. Kept local
// to the loop (render.go owns the in-frame sequences).
const (
	enterAltScreen = "\x1b[?1049h\x1b[?25l"
	leaveAltScreen = "\x1b[?25h\x1b[?1049l"
)

// hiddenPoll is how often a hidden sidebar wakes on its own to re-check
// visibility, as a backstop for a missed SIGUSR1 wake. While hidden the loop
// otherwise blocks on signals, so this bounds the worst-case latency without
// burning CPU.
const hiddenPoll = 10 * time.Second

// Ticker is the minimal clock surface the loop needs, so tests can drive time
// deterministically. time.Ticker satisfies an adapter of this shape.
type Ticker interface {
	C() <-chan time.Time
	Reset(d time.Duration)
	Stop()
}

// DB is the store surface the loop reads: the live rows plus the cheap change
// counter used to skip redundant Live() reads. *store.DB satisfies it.
type DB interface {
	Live() ([]store.Status, error)
	DataVersion() (int64, error)
}

// Deps are the loop's injected collaborators. Everything that touches tmux, the
// process table, the clock, the DB, signals, or the terminal is a field here so
// Loop is exercised in tests without any of them.
type Deps struct {
	// Self is $TMUX_PANE: the sidebar pane's own id, used to locate our row in
	// the pane list and decide visibility.
	Self string
	// Home and GitRoot feed Collect's workspace grouping.
	Home    string
	GitRoot func(string) string

	// ListPanes lists every pane (production: tmuxcli.ListSidebarPanes).
	ListPanes func() ([]tmuxcli.SidebarPane, error)
	// Snapshot returns a `ps -axo pid=,ppid=,comm=` dump for the Detector.
	Snapshot func() string
	// DB is the status store; nil when it could not be opened (render Live=nil).
	DB DB
	// Config reloads the sidebar config and reports the file's mtime (unix
	// nanoseconds, 0 when missing/unknown). Optional; nil disables live reload.
	Config func() (config.Sidebar, int64, error)

	// Size reports the render target's (width, height) in cells.
	Size func() (int, int)
	// ResizeSelf resizes the sidebar pane to width (production: ResizePaneX self).
	ResizeSelf func(width int)
	// Out receives the alternate-screen frames.
	Out io.Writer

	// Ticker drives the render cadence; Wake fires on SIGUSR1/SIGWINCH; Term on
	// SIGHUP/SIGTERM.
	Ticker Ticker
	Wake   <-chan os.Signal
	Term   <-chan os.Signal

	// Refresh is the initial visible tick interval; the config reload may change
	// it at runtime.
	Refresh time.Duration
}

// looper holds the mutable loop state between ticks. Split from Loop so tests can
// drive tick() directly, deterministically, without the select/goroutine.
type looper struct {
	deps Deps

	cfg       config.Sidebar
	lastMtime int64
	refresh   time.Duration

	det          *agents.Detector
	lastPaneSig  string
	visibleTicks int

	live            []store.Status
	liveFetched     bool
	lastDataVersion int64

	lastFrame []byte
}

// newLooper builds a looper and seeds the cached config so the first tick does
// not spuriously resize or re-read a just-loaded file.
func newLooper(deps Deps) *looper {
	l := &looper{deps: deps}
	l.cfg = config.DefaultSidebar()
	l.refresh = deps.Refresh
	if l.refresh <= 0 {
		l.refresh = time.Duration(l.cfg.RefreshMS) * time.Millisecond
	}
	if deps.Config != nil {
		if cfg, mtime, err := deps.Config(); err == nil {
			l.cfg = cfg
			l.lastMtime = mtime
			l.refresh = time.Duration(cfg.RefreshMS) * time.Millisecond
		}
	}
	return l
}

// Loop runs the sidebar render loop until a terminate signal, a disable, or a
// fatal error. It enters the alternate screen, renders on every visible change,
// sleeps (blocking on signals) while hidden, and always restores the terminal on
// return. It returns nil on a clean exit (SIGHUP/SIGTERM/disable).
func Loop(deps Deps) error {
	l := newLooper(deps)

	io.WriteString(deps.Out, enterAltScreen)
	defer io.WriteString(deps.Out, leaveAltScreen)

	interval := time.Duration(-1)
	for {
		visible, exit := l.tick()
		if exit {
			return nil
		}

		next := l.refresh
		if !visible {
			next = hiddenPoll
		}
		if next != interval {
			deps.Ticker.Reset(next)
			interval = next
		}

		select {
		case <-deps.Ticker.C():
		case <-deps.Wake:
		case <-deps.Term:
			return nil
		}
	}
}

// tick performs one render cycle. It returns whether the sidebar is currently
// visible (so the caller can pick the tick cadence) and whether the loop should
// exit (the sidebar was disabled). A hidden or unlistable tick does no ps/DB/render
// work.
func (l *looper) tick() (visible bool, exit bool) {
	panes, err := l.deps.ListPanes()
	if err != nil {
		return false, false
	}
	self := findSelf(panes, l.deps.Self)
	if self == nil {
		return false, false
	}

	l.reloadConfig(self)

	// A resolved-disabled sidebar exits so the loop and its RSS go away; the
	// ensure hook removes the pane.
	if !config.ResolveEnabled(self.SidebarEnabled, l.cfg) {
		return false, true
	}

	if !Visible(panes, l.deps.Self) {
		return false, false
	}
	l.visibleTicks++

	// Detector: rebuild from ps only when the pane set changed or on every 2nd
	// visible tick (CPU budget: ps dominates a tick, so it runs at most every
	// other tick). Otherwise the previous Detector is reused unchanged.
	sig := paneSignature(panes)
	paneChanged := sig != l.lastPaneSig
	l.lastPaneSig = sig
	if l.det == nil || paneChanged || l.visibleTicks%2 == 0 {
		l.det = agents.NewDetectorFromSnapshot(l.deps.Snapshot())
	}

	// Live rows: only re-read when the store changed (data_version bumped by a
	// hook writer) or the pane set changed. A DB or read error still renders,
	// with the previously-read rows (nil on the first tick).
	if l.deps.DB != nil {
		dv, dverr := l.deps.DB.DataVersion()
		changed := dverr != nil || dv != l.lastDataVersion
		if !l.liveFetched || paneChanged || changed {
			if rows, lerr := l.deps.DB.Live(); lerr == nil {
				l.live = rows
			}
			l.liveFetched = true
		}
		if dverr == nil {
			l.lastDataVersion = dv
		}
	}

	snap := Collect(Input{
		Panes:        toPaneInfo(panes),
		Det:          l.det,
		Live:         l.live,
		Home:         l.deps.Home,
		GitRoot:      l.deps.GitRoot,
		SelfWindowID: self.WindowID,
	})

	w, h := l.deps.Size()
	frame := Frame(Render(snap, w, h))
	if !bytes.Equal(frame, l.lastFrame) {
		l.deps.Out.Write(frame)
		l.lastFrame = frame
	}
	return true, false
}

// reloadConfig re-reads the config when its mtime changed, applying a width
// change (resize self) and a refresh change (new tick interval) live.
func (l *looper) reloadConfig(self *tmuxcli.SidebarPane) {
	if l.deps.Config == nil {
		return
	}
	cfg, mtime, err := l.deps.Config()
	if err != nil || mtime == l.lastMtime {
		return
	}
	l.lastMtime = mtime
	if cfg.Width != l.cfg.Width && l.deps.ResizeSelf != nil {
		l.deps.ResizeSelf(DesiredWidth(cfg, self.WindowWidth))
	}
	if cfg.RefreshMS != l.cfg.RefreshMS {
		l.refresh = time.Duration(cfg.RefreshMS) * time.Millisecond
	}
	l.cfg = cfg
}

// Visible reports whether the sidebar pane self is currently on screen: its
// window is active, its session has a client attached, and the window is not
// zoomed (a zoomed real pane hides the sidebar). A per-window @twm_sidebar_off or
// a runtime @twm_sidebar_enabled=0 also count as hidden.
func Visible(panes []tmuxcli.SidebarPane, self string) bool {
	for _, p := range panes {
		if p.PaneID != self {
			continue
		}
		if p.SidebarOff == "1" || p.SidebarEnabled == "0" {
			return false
		}
		return p.WindowActive && p.SessionAttached && !p.WindowZoomed
	}
	return false
}

// findSelf returns the pane whose id is self, or nil when it is not in the list
// (our pane was killed or not yet listed).
func findSelf(panes []tmuxcli.SidebarPane, self string) *tmuxcli.SidebarPane {
	for i := range panes {
		if panes[i].PaneID == self {
			return &panes[i]
		}
	}
	return nil
}

// paneSignature is a stable fingerprint of the real (non-sidebar) panes' ids and
// pids: it changes exactly when a process-tree root appears, disappears, or is
// replaced, which is when the Detector must be rebuilt.
func paneSignature(panes []tmuxcli.SidebarPane) string {
	parts := make([]string, 0, len(panes))
	for _, p := range panes {
		if p.Sidebar {
			continue
		}
		parts = append(parts, p.PaneID+":"+p.PanePID)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// toPaneInfo maps the tmux pane rows into Collect's PaneInfo (Collect skips the
// sidebar panes itself via the Sidebar flag).
func toPaneInfo(panes []tmuxcli.SidebarPane) []PaneInfo {
	out := make([]PaneInfo, 0, len(panes))
	for _, p := range panes {
		out = append(out, PaneInfo{
			Session:     p.Session,
			WindowIndex: p.WindowIndex,
			WindowID:    p.WindowID,
			PaneID:      p.PaneID,
			PID:         p.PanePID,
			Path:        p.Path,
			Sidebar:     p.Sidebar,
		})
	}
	return out
}

// tickerAdapter wraps a *time.Ticker as a Ticker.
type tickerAdapter struct{ t *time.Ticker }

func (a tickerAdapter) C() <-chan time.Time { return a.t.C }
func (a tickerAdapter) Reset(d time.Duration) {
	if d <= 0 {
		d = time.Second
	}
	a.t.Reset(d)
}
func (a tickerAdapter) Stop() { a.t.Stop() }

// NewTicker builds the production Ticker from the standard library clock.
func NewTicker(d time.Duration) Ticker {
	if d <= 0 {
		d = time.Second
	}
	return tickerAdapter{t: time.NewTicker(d)}
}
