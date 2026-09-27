// Package sidebar builds and renders the persistent left-hand agents panel.
//
// Collect is the pure heart of the panel: it turns a snapshot of tmux panes, a
// process-tree Detector, and the live status rows from the store into a stable,
// workspace-grouped Snapshot. It performs no I/O, so it is fully table-testable;
// the tmux/ps/DB reads that feed it live in the loop and the tmux adapters.
package sidebar

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/thaodangspace/tmux-window-manager/agents"
	"github.com/thaodangspace/tmux-window-manager/store"
)

// PaneInfo is one tmux pane as seen by Collect: enough to root a process-tree
// walk (PID), place a row (Session/WindowIndex/WindowID/PaneID/Path) and skip
// the sidebar's own panes (Sidebar).
type PaneInfo struct {
	Session     string
	WindowIndex int
	WindowID    string
	PaneID      string
	PID         string
	Path        string
	Sidebar     bool
}

// Input carries everything Collect needs. GitRoot is injected (production wires
// dirs.GitRoot) so the workspace key is testable without a filesystem.
type Input struct {
	Panes        []PaneInfo
	Det          *agents.Detector
	Live         []store.Status
	Home         string
	GitRoot      func(string) string
	SelfWindowID string
}

// Row is one agent process shown in the panel.
type Row struct {
	ID       string // agent basename, e.g. "claude"
	Display  string // human name, e.g. "Claude Code"
	Status   string // store status: running | waiting | idle
	Subtitle string // dim second line
	Target   string // tmux target "session:index"
	PaneID   string
	PID      int
	Current  bool // lives in the sidebar's own window
	Hooked   bool // has a matching live DB row
}

// Workspace groups the agents that share a git root (or cwd fallback).
type Workspace struct {
	Key    string
	Label  string
	Agents []Row
}

// Snapshot is the whole panel content, deterministically ordered.
type Snapshot struct {
	Workspaces []Workspace
}

// rowCtx keeps the sort keys (session, window index, pane id) alongside the Row
// so ordering does not depend on the incoming pane order.
type rowCtx struct {
	row     Row
	session string
	winIdx  int
	paneID  string
}

// Collect joins panes, process tree, and live status into a grouped Snapshot.
// Outermost agents per nested chain become rows; each is matched to its newest
// live DB row by pid (unmatched agents render idle/hookless, unmatched DB rows
// are dropped). Rows are keyed by git root (cwd fallback) into workspaces.
func Collect(in Input) Snapshot {
	// Newest live status per pid (later UpdatedAt wins on a shared pid).
	byPID := make(map[int]store.Status)
	for _, s := range in.Live {
		if s.Pid == 0 {
			continue
		}
		if prev, ok := byPID[s.Pid]; !ok || s.UpdatedAt >= prev.UpdatedAt {
			byPID[s.Pid] = s
		}
	}

	// Accumulate rows per workspace key, preserving a keys slice for stable
	// label collision handling.
	rowsByKey := make(map[string][]rowCtx)

	for _, pane := range in.Panes {
		if pane.Sidebar {
			continue
		}
		if in.Det == nil {
			continue
		}
		for _, g := range in.Det.AgentGroups(pane.PID) {
			target := pane.Session + ":" + strconv.Itoa(pane.WindowIndex)
			row := Row{
				ID:      g.ID,
				Display: agents.DisplayName(g.ID),
				Status:  store.Idle,
				Target:  target,
				PaneID:  pane.PaneID,
				PID:     g.PID,
				Current: pane.WindowID != "" && pane.WindowID == in.SelfWindowID,
			}

			// Join the newest live row attributed to any agent in the chain:
			// the hook records status under the nearest (possibly inner) agent's
			// pid, so match the outer agent's pid or any nested descendant's.
			cwd := pane.Path
			if best, ok := newestForMembers(byPID, g.Members); ok {
				row.Hooked = true
				row.Status = best.Status
				if best.Cwd != "" {
					cwd = best.Cwd
				}
				row.Subtitle = subtitle(best, target)
			} else {
				row.Subtitle = target
			}

			key := ""
			if in.GitRoot != nil {
				key = in.GitRoot(cwd)
			}
			if key == "" {
				key = cwd
			}

			rowsByKey[key] = append(rowsByKey[key], rowCtx{
				row:     row,
				session: pane.Session,
				winIdx:  pane.WindowIndex,
				paneID:  pane.PaneID,
			})
		}
	}

	// Resolve labels, disambiguating basename collisions across keys.
	keys := make([]string, 0, len(rowsByKey))
	for k := range rowsByKey {
		keys = append(keys, k)
	}
	labels := resolveLabels(keys, in.Home)

	workspaces := make([]Workspace, 0, len(keys))
	for _, k := range keys {
		ctxs := rowsByKey[k]
		sort.SliceStable(ctxs, func(i, j int) bool {
			if ctxs[i].session != ctxs[j].session {
				return ctxs[i].session < ctxs[j].session
			}
			if ctxs[i].winIdx != ctxs[j].winIdx {
				return ctxs[i].winIdx < ctxs[j].winIdx
			}
			return ctxs[i].paneID < ctxs[j].paneID
		})
		rows := make([]Row, len(ctxs))
		for i, c := range ctxs {
			rows[i] = c.row
		}
		workspaces = append(workspaces, Workspace{Key: k, Label: labels[k], Agents: rows})
	}

	sort.SliceStable(workspaces, func(i, j int) bool {
		if workspaces[i].Label != workspaces[j].Label {
			return workspaces[i].Label < workspaces[j].Label
		}
		return workspaces[i].Key < workspaces[j].Key
	})

	return Snapshot{Workspaces: workspaces}
}

// newestForMembers returns the live status with the newest UpdatedAt among the
// given member pids (each already deduped to its own newest row in byPID). ok is
// false when none of the members has a live row.
func newestForMembers(byPID map[int]store.Status, members []int) (store.Status, bool) {
	var best store.Status
	found := false
	for _, pid := range members {
		st, ok := byPID[pid]
		if !ok {
			continue
		}
		if !found || st.UpdatedAt >= best.UpdatedAt {
			best = st
			found = true
		}
	}
	return best, found
}

// subtitle picks the dim second line: a waiting Detail, else the first line of
// the current turn prompt, else the tmux target.
func subtitle(s store.Status, target string) string {
	if s.Status == store.Waiting && s.Detail != "" {
		return s.Detail
	}
	if s.TurnPrompt != "" {
		return firstLine(s.TurnPrompt)
	}
	return target
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// resolveLabels maps each key to a display label: "~" for $HOME, else the
// basename, disambiguated as "name (parent)" when two keys share a basename.
func resolveLabels(keys []string, home string) map[string]string {
	cleanHome := ""
	if home != "" {
		cleanHome = filepath.Clean(home)
	}

	base := make(map[string]string, len(keys))
	counts := make(map[string]int)
	for _, k := range keys {
		var b string
		if cleanHome != "" && filepath.Clean(k) == cleanHome {
			b = "~"
		} else {
			b = filepath.Base(k)
		}
		base[k] = b
		counts[b]++
	}

	out := make(map[string]string, len(keys))
	for _, k := range keys {
		b := base[k]
		if counts[b] > 1 && b != "~" {
			parent := filepath.Base(filepath.Dir(k))
			out[k] = b + " (" + parent + ")"
		} else {
			out[k] = b
		}
	}
	return out
}
