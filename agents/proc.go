// Package agents detects which coding agents (claude/codex/pi) are running in a
// tmux window and whether they are busy — natively, with no dependency on the
// external `t2` tool. It replaces the awk process-tree walk and the cached
// `t2 agents --format json` lookups of the original tmux_window_manager.sh.
package agents

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// agentRe matches the process basenames we treat as coding agents. The set is
// identical to the original script's `^(claude|codex|pi)$`.
var agentRe = regexp.MustCompile(`^(claude|codex|pi)$`)

// proc is one row of the process snapshot, kept in `ps` output order so agent
// names are reported in a stable, reproducible sequence.
type proc struct {
	pid  string
	ppid string
	name string // command basename
}

// Detector holds a single process snapshot, reused across every lookup so we
// run `ps` once (matching the script's one-shot $ps_snap), not per pane.
type Detector struct {
	order    []proc              // processes in ps output order
	children map[string][]string // ppid -> child pids
}

// NewDetector captures the current process table via
// `ps -axo pid=,ppid=,comm=`. On failure it returns an empty detector that
// reports no agents, so callers degrade gracefully.
func NewDetector() *Detector {
	out, _ := exec.Command("ps", "-axo", "pid=,ppid=,comm=").Output()
	return newDetectorFromSnapshot(string(out))
}

// NewDetectorFromSnapshot builds a Detector from a `ps -axo pid=,ppid=,comm=`
// dump instead of the live process table. Useful for tests and callers that
// already hold a snapshot.
func NewDetectorFromSnapshot(snapshot string) *Detector {
	return newDetectorFromSnapshot(snapshot)
}

// newDetectorFromSnapshot parses a `ps -axo pid=,ppid=,comm=` dump. Split out
// for table-driven testing without a real process table.
func newDetectorFromSnapshot(snapshot string) *Detector {
	d := &Detector{children: make(map[string][]string)}
	for _, line := range strings.Split(snapshot, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, ppid := fields[0], fields[1]
		// comm may be a full path (macOS) and could in theory contain spaces,
		// so rejoin everything past ppid before taking the basename — matching
		// the awk `sub(/.*\//,"",c)`.
		name := basename(strings.Join(fields[2:], " "))
		d.order = append(d.order, proc{pid: pid, ppid: ppid, name: name})
		d.children[ppid] = append(d.children[ppid], pid)
	}
	return d
}

// Names returns the distinct agent basenames found anywhere in the process
// subtree rooted at the given pids (the pids themselves included), in ps order.
// This is the native port of the awk descendant-marking loop.
func (d *Detector) Names(rootPIDs ...string) []string {
	if len(rootPIDs) == 0 || len(d.order) == 0 {
		return nil
	}
	want := d.descendants(rootPIDs)

	// Collect distinct agent names in ps output order.
	var out []string
	seen := make(map[string]bool)
	for _, p := range d.order {
		if want[p.pid] && agentRe.MatchString(p.name) && !seen[p.name] {
			seen[p.name] = true
			out = append(out, p.name)
		}
	}
	return out
}

// AgentPIDs returns the pids of every agent process anywhere in the subtree(s)
// rooted at the given pids (the roots included), in ps order. Where Names
// answers "which agents run here", AgentPIDs answers "which agent processes run
// here" — letting the reader match a recorded status row (which carries its
// agent's pid) to the exact window that process lives in, instead of guessing by
// shared working directory.
func (d *Detector) AgentPIDs(rootPIDs ...string) []int {
	if len(rootPIDs) == 0 || len(d.order) == 0 {
		return nil
	}
	want := d.descendants(rootPIDs)

	var out []int
	for _, p := range d.order {
		if want[p.pid] && agentRe.MatchString(p.name) {
			if pid, err := strconv.Atoi(p.pid); err == nil {
				out = append(out, pid)
			}
		}
	}
	return out
}

// descendants returns the set of pids in the subtree(s) rooted at rootPIDs (the
// roots included) — the marking step shared by Names and AgentPIDs.
func (d *Detector) descendants(rootPIDs []string) map[string]bool {
	want := make(map[string]bool, len(rootPIDs))
	queue := make([]string, 0, len(rootPIDs))
	for _, pid := range rootPIDs {
		if pid != "" && !want[pid] {
			want[pid] = true
			queue = append(queue, pid)
		}
	}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		for _, child := range d.children[pid] {
			if !want[child] {
				want[child] = true
				queue = append(queue, child)
			}
		}
	}
	return want
}

// byPID indexes the snapshot by pid for ancestor walks.
func (d *Detector) byPID() map[string]proc {
	m := make(map[string]proc, len(d.order))
	for _, p := range d.order {
		m[p.pid] = p
	}
	return m
}

// NearestAgent walks up the process tree from startPID and returns the pid and
// basename of the closest ancestor (startPID itself included) that is a coding
// agent. It is how the hook handler — which runs as a short-lived child of the
// agent — discovers which agent process to attribute its event to, so the
// reader can later gate on that pid's liveness. Returns (0, "") when no agent
// ancestor is found or the walk loops.
func (d *Detector) NearestAgent(startPID string) (int, string) {
	if startPID == "" || len(d.order) == 0 {
		return 0, ""
	}
	index := d.byPID()
	seen := make(map[string]bool)
	for cur := startPID; cur != "" && cur != "0" && !seen[cur]; {
		seen[cur] = true
		p, ok := index[cur]
		if !ok {
			break
		}
		if agentRe.MatchString(p.name) {
			pid, _ := strconv.Atoi(p.pid)
			return pid, p.name
		}
		cur = p.ppid
	}
	return 0, ""
}

func basename(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}
