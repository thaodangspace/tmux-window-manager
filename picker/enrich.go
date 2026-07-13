package picker

import (
	"strconv"
	"strings"

	"github.com/dtonair/tmux-window-manager/agents"
	"github.com/dtonair/tmux-window-manager/store"
	"github.com/dtonair/tmux-window-manager/tmuxcli"
)

// LiveEnricher decorates the window list with agent badges. The agent *name*
// comes from the live process table (the detector sees which of claude/codex/pi
// actually run in each window's panes), so an agent shows up even when its hooks
// haven't reported — e.g. a Codex session, whose only hook fires at turn end.
//
// The status DB then *enriches* that badge: agents push lifecycle status
// (idle/running/waiting) and model via hooks, each row carrying the pid of the
// agent process it belongs to. We match those rows to a window by pid, not by
// the pane's working directory — pid matching is what keeps a codex window and a
// claude window distinct when they share a directory (directory matching
// collapsed both onto whichever reported last).
type LiveEnricher struct {
	live    []store.Status           // live status rows, for model/status enrichment
	windows map[string]*windowAgents // "session:index" -> agents detected in its panes
}

// windowAgents is the set of agents detected across a window's panes: their
// distinct names (in process-table order) and the pids used to match status rows.
type windowAgents struct {
	names []string
	pids  pidSet
}

type pidSet map[int]bool

func (wa *windowAgents) add(names []string, pids []int) {
	for _, n := range names {
		if !contains(wa.names, n) {
			wa.names = append(wa.names, n)
		}
	}
	for _, pid := range pids {
		wa.pids[pid] = true
	}
}

// NewLiveEnricher builds the enricher from the live status rows, the current
// pane snapshot, and a fresh process-table detector.
func NewLiveEnricher(live []store.Status) *LiveEnricher {
	return newLiveEnricher(live, tmuxcli.AllPanes(), agents.NewDetector())
}

// newLiveEnricher is the testable core: rows, panes, and detector are injected
// so the detection/pid-matching logic can be exercised without a live tmux or
// process table.
func newLiveEnricher(live []store.Status, panes []tmuxcli.Pane, det *agents.Detector) *LiveEnricher {
	e := &LiveEnricher{
		live:    live,
		windows: map[string]*windowAgents{},
	}
	for _, p := range panes {
		names := det.Names(p.PID)
		if len(names) == 0 {
			continue
		}
		key := p.Session + ":" + strconv.Itoa(p.WindowIndex)
		wa := e.windows[key]
		if wa == nil {
			wa = &windowAgents{pids: pidSet{}}
			e.windows[key] = wa
		}
		wa.add(names, det.AgentPIDs(p.PID))
	}
	return e
}

func (e *LiveEnricher) Window(session string, index int, _, _ string) WindowBadge {
	wa := e.windows[session+":"+strconv.Itoa(index)]
	if wa == nil || len(wa.names) == 0 {
		return WindowBadge{}
	}
	model, status := e.enrich(wa.pids)
	paneLabel := strings.Join(wa.names, " ")
	label := paneLabel
	if model != "" {
		label += "(" + model + ")"
	}
	return WindowBadge{AgentLabel: label, PaneLabel: paneLabel, Status: status}
}

// enrich folds the live status rows whose agent pid runs in this window into the
// model string (distinct models joined) and the most attention-worthy status
// (waiting beats running beats idle). Rows with an unknown pid (0) can't be
// attributed to a window and are skipped; a window with no matching row keeps an
// empty model/status, so its badge still shows the detected agent name alone.
func (e *LiveEnricher) enrich(pids pidSet) (model, status string) {
	var ms []string
	seen := map[string]bool{}
	for _, s := range e.live {
		if s.Pid == 0 || !pids[s.Pid] {
			continue
		}
		if s.Model != "" && !seen[s.Model] {
			seen[s.Model] = true
			ms = append(ms, s.Model)
		}
		status = moreUrgent(status, s.Status)
	}
	return strings.Join(ms, "/"), status
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// statusRank orders states by how much they want the user's attention.
func statusRank(s string) int {
	switch s {
	case store.Waiting:
		return 3
	case store.Running:
		return 2
	case store.Idle:
		return 1
	default:
		return 0
	}
}

// moreUrgent returns whichever status ranks higher.
func moreUrgent(a, b string) string {
	if statusRank(b) > statusRank(a) {
		return b
	}
	return a
}
