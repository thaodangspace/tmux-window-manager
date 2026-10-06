package cli

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/tmux-window-manager/agents"
	"github.com/thaodangspace/tmux-window-manager/notify"
	"github.com/thaodangspace/tmux-window-manager/store"
	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
)

// judgeBackoff spaces out retries while the model endpoint is failing.
const judgeBackoff = 30 * time.Second

// serverGoneScans is how many scans in a row must find no tmux server before
// the watcher exits, so one failed `tmux list-sessions` (sleep/wake, a busy
// server) does not end it.
const serverGoneScans = 3

var errWatcherRunning = errors.New("watcher already running")

// newWatchCommand runs the pane watcher, the single source of agent status
// and notifications. Every scan it captures each pane running a coding
// agent: a changing screen is `working`; a screen that settles is classified
// by the local model (notify.Judge), written to the status DB for the picker,
// and delivered through the configured backends when it needs the user.
func newWatchCommand() *cobra.Command {
	var (
		detach, replace, once bool
		socket                string
	)
	cmd := &cobra.Command{
		Use:    "watch [--detach] [--replace] [--once] [--socket S]",
		Short:  "Watch agent panes: status for the picker, notifications",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if replace {
				stopWatcher()
			}
			cfg, judgeOn, err := notify.LoadPollerConfig()
			if err != nil {
				debugf("watch: config failed (%s); model judge off", notifyErrorCategory(err))
			}
			if socket == "" {
				socket = tmuxcli.SocketFromEnv(os.Getenv("TMUX"))
			} else {
				tmuxcli.Socket = socket
			}
			if once {
				if !judgeOn {
					return errors.New("[poller] is not enabled in twm.toml")
				}
				newWatcher(cfg, true, socket, nil).once(cmd.Context(), cmd.OutOrStdout())
				return nil
			}
			if detach {
				self, err := os.Executable()
				if err != nil {
					return err
				}
				argv := []string{self, "watch"}
				if socket != "" {
					argv = append(argv, "--socket", socket)
				}
				return spawnDetached(argv)
			}
			// Signals before the lock: stopWatcher may target us as soon as
			// the lock names our pid.
			stop := make(chan os.Signal, 1)
			signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
			lock, err := acquireWatchLock()
			if errors.Is(err, errWatcherRunning) {
				return nil
			}
			if err != nil {
				return err
			}
			defer lock.Close()
			db, err := store.Open()
			if err != nil {
				return err
			}
			defer db.Close()
			// Rows from hooks of older releases, or from a previous watcher,
			// are rebuilt from the panes.
			if rows, err := db.All(); err == nil {
				for _, r := range rows {
					_ = db.Delete(r.Agent, r.SessionID)
				}
			}
			newWatcher(cfg, judgeOn, socket, db).run(stop)
			return nil
		},
	}
	cmd.Flags().BoolVar(&detach, "detach", false, "start the watcher in the background and return")
	cmd.Flags().BoolVar(&replace, "replace", false, "stop a running watcher first")
	cmd.Flags().BoolVar(&once, "once", false, "judge every agent pane once and print the verdicts (no writes, no delivery)")
	cmd.Flags().StringVar(&socket, "socket", "", "tmux server socket path")
	return cmd
}

// agentPane is one tmux pane running a coding agent.
type agentPane struct {
	ID    string
	Agent string // outermost agent basename
	PID   int    // outermost agent pid; the picker joins status rows on it
	Cwd   string
}

// paneState is what the watcher remembers about one pane between scans.
type paneState struct {
	hash       string    // normalized screen at the last scan
	changedAt  time.Time // when hash last changed
	busySince  time.Time // when the pane last turned working
	judgedHash string
	judgedAt   time.Time
	failedAt   time.Time
	// notifiedHash is the screen last considered for a notification, so one
	// settled screen notifies at most once.
	notifiedHash string

	// last written status row
	status, detail, model string
	pid                   int
}

type watcher struct {
	cfg     notify.PollerConfig
	judgeOn bool
	socket  string
	states  map[string]*paneState
	written map[string]string // pane id -> agent of rows we wrote
	seeded  bool

	// seams
	now      func() time.Time
	serverUp func() bool
	panes    func() []agentPane
	capture  func(pane string) (string, error)
	lookup   func(pane string) (tmuxcli.PaneFocus, bool)
	judge    func(ctx context.Context, agent, screen string) (notify.Verdict, error)
	backends backendFactory
	upsert   func(store.Status)
	remove   func(agent, sessionID string)
}

func newWatcher(cfg notify.PollerConfig, judgeOn bool, socket string, db *store.DB) *watcher {
	w := &watcher{
		cfg:     cfg,
		judgeOn: judgeOn,
		socket:  socket,
		states:  map[string]*paneState{},
		now:     time.Now,
		serverUp: func() bool {
			return len(tmuxcli.ListSessions()) > 0
		},
		panes:   listAgentPanes,
		capture: func(pane string) (string, error) { return tmuxcli.CapturePane(pane, false) },
		lookup: func(pane string) (tmuxcli.PaneFocus, bool) {
			return tmuxcli.LookupPane(pane, time.Now(), focusRecent)
		},
		judge: func(ctx context.Context, agent, screen string) (notify.Verdict, error) {
			return notify.Judge(ctx, cfg, agent, screen)
		},
		backends: notifiersFromConfig,
		upsert:   func(store.Status) {},
		remove:   func(string, string) {},
	}
	if db != nil {
		w.upsert = func(s store.Status) {
			if err := db.Upsert(s); err != nil {
				debugf("watch: upsert: %v", err)
			}
		}
		w.remove = func(agent, sessionID string) { _ = db.Delete(agent, sessionID) }
	}
	return w
}

// run scans until stop fires or the tmux server is gone for serverGoneScans
// scans in a row. stop cancels an in-flight model call, so `--replace` never
// waits on a slow judge.
func (w *watcher) run(stop <-chan os.Signal) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-stop
		cancel()
	}()
	ticker := time.NewTicker(w.cfg.Scan)
	defer ticker.Stop()
	misses := 0
	for {
		if w.serverUp() {
			misses = 0
			w.tick(ctx)
		} else if misses++; misses >= serverGoneScans {
			debugf("watch: tmux server gone, exiting")
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

func paneSessionID(pane string) string { return "pane:" + pane }

// tick captures every agent pane once. A changed screen marks the pane
// working; a screen unchanged for Settle is judged once, and its verdict
// becomes the pane's status and, when it needs the user, a notification.
func (w *watcher) tick(ctx context.Context) {
	defer func() {
		if recover() != nil {
			debugf("watch: tick failed (panic)")
		}
	}()
	first := !w.seeded
	w.seeded = true

	seen := map[string]bool{}
	for _, ap := range w.panes() {
		if ctx.Err() != nil {
			return // stopping: keep states and rows as they are
		}
		seen[ap.ID] = true
		raw, err := w.capture(ap.ID)
		if err != nil {
			continue
		}
		screen := tailLines(raw, w.cfg.Lines)
		hash := screenHash(screen)
		now := w.now()

		st := w.states[ap.ID]
		if st != nil && st.status != "" && ap.PID != st.pid {
			// Agent restarted in place: re-point the row at the new pid.
			w.write(ap, st, st.status, st.detail, st.model)
		}
		switch {
		case st == nil:
			st = &paneState{hash: hash, changedAt: now}
			if first {
				st.notifiedHash = hash // starting the watcher is silent
			}
			w.states[ap.ID] = st
		case hash != st.hash:
			st.hash, st.changedAt = hash, now
			if st.status != store.Working {
				st.busySince = now
			}
			w.write(ap, st, store.Working, "", "")
			continue
		}

		if now.Sub(st.changedAt) < w.cfg.Settle {
			continue
		}
		due := st.judgedHash != hash ||
			(st.status == store.Working && now.Sub(st.judgedAt) >= w.cfg.Interval)
		if !due {
			continue
		}
		if !w.judgeOn {
			st.judgedHash, st.judgedAt = hash, now
			w.write(ap, st, store.Idle, "", "")
			continue
		}
		if !st.failedAt.IsZero() && now.Sub(st.failedAt) < judgeBackoff {
			continue
		}
		v, err := w.judge(ctx, ap.Agent, screen)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			debugf("watch: judge %s failed (%s)", ap.ID, notifyErrorCategory(err))
			st.failedAt = now
			if st.status == store.Working || st.status == "" {
				w.write(ap, st, store.Idle, "", "") // settled, state unknown
			}
			continue
		}
		st.failedAt = time.Time{}
		st.judgedHash, st.judgedAt = hash, now
		debugf("watch: %s %s %s", ap.ID, ap.Agent, v.State)
		w.write(ap, st, statusFor(v.State), v.Summary, v.Model)

		if !v.Notify() || st.notifiedHash == hash {
			continue
		}
		st.notifiedHash = hash
		w.notify(ap, st, v)
	}
	for id := range w.states {
		if !seen[id] {
			delete(w.states, id)
		}
	}
	w.reapRows(seen)
}

// reapRows deletes the rows of panes that no longer run an agent.
func (w *watcher) reapRows(seen map[string]bool) {
	for id, st := range w.written {
		if !seen[id] {
			w.remove(st, paneSessionID(id))
			delete(w.written, id)
		}
	}
}

func statusFor(state string) string {
	switch state {
	case notify.ScreenWorking:
		return store.Working
	case notify.ScreenWaiting:
		return store.Waiting
	case notify.ScreenError:
		return store.Error
	default:
		return store.Idle
	}
}

// write records the pane's status row when anything the picker shows changed.
func (w *watcher) write(ap agentPane, st *paneState, status, detail, model string) {
	if model == "" {
		model = st.model
	}
	if status == st.status && detail == st.detail && model == st.model && ap.PID == st.pid {
		return
	}
	st.status, st.detail, st.model, st.pid = status, detail, model, ap.PID
	if w.written == nil {
		w.written = map[string]string{}
	}
	w.written[ap.ID] = ap.Agent
	w.upsert(store.Status{
		Agent:     ap.Agent,
		SessionID: paneSessionID(ap.ID),
		Cwd:       ap.Cwd,
		Pid:       ap.PID,
		Status:    status,
		Detail:    detail,
		Model:     model,
		Latest:    detail,
		UpdatedAt: w.now().UnixMilli(),
	})
}

func (w *watcher) notify(ap agentPane, st *paneState, v notify.Verdict) {
	if w.backends == nil {
		return
	}
	backends := w.backends()
	if len(backends) == 0 {
		return
	}
	focus, _ := w.lookup(ap.ID)
	e := notify.Event{
		Agent:      agents.DisplayName(ap.Agent),
		Cwd:        ap.Cwd,
		SessionID:  "pane" + strings.TrimPrefix(ap.ID, "%"),
		Location:   focus.Location,
		Model:      st.model,
		Pane:       ap.ID,
		TmuxSocket: w.socket,
	}
	var busy time.Duration
	if !st.busySince.IsZero() {
		busy = st.changedAt.Sub(st.busySince)
	}
	switch v.State {
	case notify.ScreenCompleted:
		e.Kind = notify.Completed
		e.Duration = busy
	default: // waiting, error
		e.Kind = notify.Waiting
		e.Detail = v.Summary
	}
	deliver(backends, e, focus.Watched, v.Summary, func(opts notify.Options) string {
		if e.Kind == notify.Completed && opts.MinTurn > 0 && busy > 0 && busy < opts.MinTurn {
			return "short turn"
		}
		return ""
	})
}

// once judges every agent pane regardless of state and prints the verdicts.
func (w *watcher) once(ctx context.Context, out io.Writer) {
	for _, ap := range w.panes() {
		raw, err := w.capture(ap.ID)
		if err != nil {
			continue
		}
		focus, _ := w.lookup(ap.ID)
		v, err := w.judge(ctx, ap.Agent, tailLines(raw, w.cfg.Lines))
		if err != nil {
			fmt.Fprintf(out, "%s\t%s\t%s\terror: %v\n", ap.ID, focus.Location, ap.Agent, err)
			continue
		}
		fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\t%s\n", ap.ID, focus.Location, ap.Agent, v.State, v.Model, v.Summary)
	}
}

// listAgentPanes returns every pane whose process tree runs a coding agent.
func listAgentPanes() []agentPane {
	panes := tmuxcli.AllPanes()
	if len(panes) == 0 {
		return nil
	}
	d := agents.NewDetector()
	var out []agentPane
	for _, pane := range panes {
		groups := d.AgentGroups(pane.PID)
		if len(groups) == 0 {
			continue
		}
		out = append(out, agentPane{ID: pane.ID, Agent: groups[0].ID, PID: groups[0].PID, Cwd: pane.Path})
	}
	return out
}

// tailLines returns the last n lines of a capture with trailing whitespace
// and trailing blank lines removed.
func tailLines(raw string, n int) string {
	lines := strings.Split(raw, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if n > 0 && len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// screenHash ignores digits so elapsed-time and token counters on an agent's
// status line do not count as activity.
func screenHash(screen string) string {
	stripped := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return -1
		}
		return r
	}, screen)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(stripped)))
}

// watchLockPath sits next to the status DB so every environment that opens
// the DB agrees on it regardless of $TMPDIR.
func watchLockPath() (string, error) {
	db, err := store.Path()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(db), "watch.pid"), nil
}

// acquireWatchLock takes the exclusive watcher lock and records our pid in
// it. The lock is released when the process exits.
func acquireWatchLock() (*os.File, error) {
	path, err := watchLockPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, errWatcherRunning
	}
	_ = f.Truncate(0)
	_, _ = f.WriteAt([]byte(strconv.Itoa(os.Getpid())), 0)
	return f, nil
}

// runningWatcherPID returns the pid of the watcher holding the lock, or 0. A
// pid is trusted only while its lock is held, so a stale file is ignored.
func runningWatcherPID() int {
	path, err := watchLockPath()
	if err != nil {
		return 0
	}
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	if syscall.Flock(int(f.Fd()), syscall.LOCK_SH|syscall.LOCK_NB) == nil {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		return 0
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		return 0
	}
	return pid
}

// stopWatcher terminates a running watcher and waits briefly for its lock.
func stopWatcher() {
	pid := runningWatcherPID()
	if pid == 0 || syscall.Kill(pid, syscall.SIGTERM) != nil {
		return
	}
	for i := 0; i < 100 && runningWatcherPID() != 0; i++ {
		time.Sleep(100 * time.Millisecond)
	}
}
