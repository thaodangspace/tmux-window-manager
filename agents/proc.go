// Package agents detects which coding agents (claude/codex/pi) are running in a
// tmux window and whether they are busy — natively, with no dependency on the
// external `t2` tool. It replaces the awk process-tree walk and the cached
// `t2 agents --format json` lookups of the original tmux_window_manager.sh.
package agents

import (
	"os/exec"
	"strconv"
	"strings"
)

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
	order      []proc              // processes in ps output order
	children   map[string][]string // ppid -> child pids
	foreground map[string]bool     // agent pids in their terminal's foreground process group
}

// NewDetector captures the current process table via
// `ps -axo pid=,ppid=,comm=`. On failure it returns an empty detector that
// reports no agents, so callers degrade gracefully.
func NewDetector() *Detector {
	out, _ := exec.Command("ps", "-axo", "pid=,ppid=,comm=").Output()
	d := newDetectorFromSnapshot(string(out))
	d.resolveScripts(func(pids []string) string {
		out, _ := exec.Command("ps", "-o", "pid=,args=", "-p", strings.Join(pids, ",")).Output()
		return string(out)
	})
	d.resolveForeground(func(pids []string) string {
		out, _ := exec.Command("ps", "-o", "pid=,pgid=,tpgid=", "-p", strings.Join(pids, ",")).Output()
		return string(out)
	})
	return d
}

// interpreters run agents shipped as scripts (gemini, aider, ...), whose
// process name is the runtime rather than the agent.
var interpreters = map[string]bool{
	"node": true, "bun": true, "deno": true, "python": true, "python3": true,
}

// resolveScripts renames interpreter processes after the script they run when
// that script is a known agent (`node /opt/homebrew/bin/gemini` -> gemini).
// args fetches `ps -o pid=,args=` for the given pids in one call.
func (d *Detector) resolveScripts(args func(pids []string) string) {
	var pids []string
	for _, p := range d.order {
		if interpreters[p.name] || strings.HasPrefix(p.name, "python3.") {
			pids = append(pids, p.pid)
		}
	}
	if len(pids) == 0 {
		return
	}
	scripts := map[string]string{}
	for _, line := range strings.Split(args(pids), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		// fields[1] is the interpreter; the rest are its arguments.
		if name := scriptAgent(fields[2:]); name != "" {
			scripts[fields[0]] = name
		}
	}
	for i, p := range d.order {
		if name, ok := scripts[p.pid]; ok {
			d.order[i].name = name
		}
	}
}

// packageAgents maps npm package names that differ from their agent id.
var packageAgents = map[string]string{
	"gemini-cli":  "gemini",
	"qwen-code":   "qwen",
	"claude-code": "claude",
}

// scriptAgent returns the agent an interpreter's arguments run, or "". The first
// positional argument is the script; later ones count only when they are paths
// or package specs (`npx @openai/codex`, `node --require x.js /bin/gemini`), so a
// bare word such as `--model claude` is not mistaken for an agent.
func scriptAgent(args []string) string {
	positional := 0
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			continue
		}
		positional++
		if positional > 1 && !strings.Contains(a, "/") {
			continue
		}
		if name := pathAgent(a); name != "" {
			return name
		}
	}
	return ""
}

// pathAgent names the agent a script path or package spec belongs to: by its
// basename (gemini.js, codex@latest, gemini-cli) or, for an entry point inside
// an installed package (.../node_modules/@google/gemini-cli/dist/index.js), by
// that package's name.
func pathAgent(p string) string {
	if id := agentID(basename(p)); id != "" {
		return id
	}
	parts := strings.Split(p, "/")
	for i := 0; i+1 < len(parts); i++ {
		if parts[i] != "node_modules" {
			continue
		}
		pkg := parts[i+1]
		if strings.HasPrefix(pkg, "@") && i+2 < len(parts) {
			pkg = parts[i+2]
		}
		if id := agentID(pkg); id != "" {
			return id
		}
	}
	return ""
}

// agentID strips a version (`@latest`) and extension (`.js`) from name and
// returns the agent it names, or "".
func agentID(name string) string {
	if i := strings.LastIndex(name, "@"); i > 0 {
		name = name[:i]
	}
	if i := strings.LastIndex(name, "."); i > 0 {
		name = name[:i]
	}
	if IsAgent(name) {
		return name
	}
	return packageAgents[name]
}

// resolveForeground records which agent processes belong to their terminal's
// foreground process group (pgid == tpgid), so a pane running several agents
// can be attributed to the one on screen. stat fetches `ps -o pid=,pgid=,tpgid=`
// for the given pids in one call.
func (d *Detector) resolveForeground(stat func(pids []string) string) {
	var pids []string
	for _, p := range d.order {
		if IsAgent(p.name) {
			pids = append(pids, p.pid)
		}
	}
	if len(pids) == 0 {
		return
	}
	d.foreground = map[string]bool{}
	for _, line := range strings.Split(stat(pids), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		if tpgid, err := strconv.Atoi(fields[2]); err == nil && tpgid > 0 && fields[1] == fields[2] {
			d.foreground[fields[0]] = true
		}
	}
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

// Names returns the distinct basenames of the outermost agents in the process
// subtree rooted at the given pids (the pids themselves included), in ps order.
// A nested agent (codex launched by claude) is left out, so labels agree with
// the watcher, which attributes a pane to its outermost agent.
func (d *Detector) Names(rootPIDs ...string) []string {
	if len(rootPIDs) == 0 || len(d.order) == 0 {
		return nil
	}
	want := d.descendants(rootPIDs)
	index := d.byPID()

	// Collect distinct agent names in ps output order.
	var out []string
	seen := make(map[string]bool)
	for _, p := range d.order {
		if want[p.pid] && IsAgent(p.name) && !seen[p.name] && !d.hasAgentAncestor(p, want, index) {
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
		if want[p.pid] && IsAgent(p.name) {
			if pid, err := strconv.Atoi(p.pid); err == nil {
				out = append(out, pid)
			}
		}
	}
	return out
}

// AgentProc identifies one agent process found in a subtree: its pid, its agent
// id (process basename), and whether it is the outermost agent of its nested
// chain — that is, whether it has no agent ancestor within the queried subtree.
type AgentProc struct {
	PID   int
	ID    string
	Outer bool
}

// Agents returns every agent process in the subtree(s) rooted at rootPIDs (the
// roots included), in ps output order, tagging each with whether it is the
// outermost agent of its nested chain. A nested agent (e.g. codex launched by
// claude) is reported with Outer=false while its enclosing claude is Outer=true;
// sibling agents are each Outer=true. It reuses the same subtree marking as
// Names/AgentPIDs plus the byPID index, adding only a cheap bounded ancestor walk
// per agent rather than a second tree walk.
func (d *Detector) Agents(rootPIDs ...string) []AgentProc {
	if len(rootPIDs) == 0 || len(d.order) == 0 {
		return nil
	}
	want := d.descendants(rootPIDs)
	index := d.byPID()

	var out []AgentProc
	for _, p := range d.order {
		if !want[p.pid] || !IsAgent(p.name) {
			continue
		}
		pid, err := strconv.Atoi(p.pid)
		if err != nil {
			continue
		}
		out = append(out, AgentProc{
			PID:   pid,
			ID:    p.name,
			Outer: !d.hasAgentAncestor(p, want, index),
		})
	}
	return out
}

// AgentGroup is one outermost agent together with every agent pid in its nested
// chain (the outer agent's own pid included), all within the queried subtree.
// The watcher attributes a pane to its outermost agent, so a codex launched by
// claude shows as the outer claude; Members lets a caller match any pid in the
// chain.
type AgentGroup struct {
	PID     int    // outermost agent's pid
	ID      string // outermost agent's basename
	Members []int  // every agent pid in the chain (PID included), in ps order
	// Foreground reports whether any process in the chain is in its terminal's
	// foreground process group (always false for snapshot-built detectors).
	Foreground bool
}

// AgentGroups returns the agent processes in the subtree(s) rooted at rootPIDs
// grouped by their outermost agent, in ps output order (outer agents ordered by
// first appearance). Each group's Members lists the outer agent and every nested
// agent below it, so a status row keyed by any inner pid can be matched to the
// outer row. It reuses the same subtree marking and byPID index as Agents, with
// only the bounded per-agent ancestor walk, not a second tree walk.
func (d *Detector) AgentGroups(rootPIDs ...string) []AgentGroup {
	if len(rootPIDs) == 0 || len(d.order) == 0 {
		return nil
	}
	want := d.descendants(rootPIDs)
	index := d.byPID()

	var order []int
	groups := make(map[int]*AgentGroup)
	for _, p := range d.order {
		if !want[p.pid] || !IsAgent(p.name) {
			continue
		}
		pid, err := strconv.Atoi(p.pid)
		if err != nil {
			continue
		}
		outer := d.outerAgent(p, want, index)
		opid, err := strconv.Atoi(outer.pid)
		if err != nil {
			continue
		}
		g, ok := groups[opid]
		if !ok {
			g = &AgentGroup{PID: opid, ID: outer.name}
			groups[opid] = g
			order = append(order, opid)
		}
		g.Members = append(g.Members, pid)
		if d.foreground[p.pid] {
			g.Foreground = true
		}
	}

	if len(order) == 0 {
		return nil
	}
	out := make([]AgentGroup, 0, len(order))
	for _, opid := range order {
		out = append(out, *groups[opid])
	}
	return out
}

// PaneAgent returns the agent a pane rooted at rootPID is attributed to: the
// group in the terminal's foreground, else the first group in ps order. ok is
// false when the pane runs no agent.
func (d *Detector) PaneAgent(rootPID string) (g AgentGroup, ok bool) {
	groups := d.AgentGroups(rootPID)
	if len(groups) == 0 {
		return AgentGroup{}, false
	}
	for _, g := range groups {
		if g.Foreground {
			return g, true
		}
	}
	return groups[0], true
}

// outerAgent returns the outermost agent process at or above p within the queried
// subtree (want): the highest agent ancestor, or p itself when p has no agent
// ancestor. The walk mirrors hasAgentAncestor but keeps climbing to find the top.
func (d *Detector) outerAgent(p proc, want map[string]bool, index map[string]proc) proc {
	outer := p
	seen := map[string]bool{p.pid: true}
	for cur := p.ppid; cur != "" && cur != "0" && want[cur] && !seen[cur]; {
		seen[cur] = true
		anc, ok := index[cur]
		if !ok {
			break
		}
		if IsAgent(anc.name) {
			outer = anc
		}
		cur = anc.ppid
	}
	return outer
}

// hasAgentAncestor reports whether p has an agent process among its ancestors
// within the queried subtree (want), walking up via the pid index. The walk stops
// at the subtree root so agents living above the queried pane do not count.
func (d *Detector) hasAgentAncestor(p proc, want map[string]bool, index map[string]proc) bool {
	seen := map[string]bool{p.pid: true}
	for cur := p.ppid; cur != "" && cur != "0" && want[cur] && !seen[cur]; {
		seen[cur] = true
		anc, ok := index[cur]
		if !ok {
			break
		}
		if IsAgent(anc.name) {
			return true
		}
		cur = anc.ppid
	}
	return false
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

func basename(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}
