package sidebar

import (
	"strconv"
	"strings"

	"github.com/thaodangspace/tmux-window-manager/config"
)

// minSidebarWidth is the floor below which no sidebar is created: a pane
// narrower than this cannot show a useful row, so very narrow windows go
// without one.
const minSidebarWidth = 10

// ActionType enumerates the tmux mutations Decide can request.
type ActionType int

const (
	// ActionCreate docks a new sidebar pane on the left of WindowID at Width.
	ActionCreate ActionType = iota
	// ActionKill removes the sidebar pane PaneID.
	ActionKill
	// ActionKillWindow closes WindowID (its last real pane is gone).
	ActionKillWindow
	// ActionResize sets sidebar pane PaneID to Width.
	ActionResize
	// ActionUnzoom unzooms the window via sidebar pane PaneID.
	ActionUnzoom
)

// Action is one tmux mutation the ensure executor should perform. WindowID is
// set for Create/KillWindow; PaneID for Kill/Resize/Unzoom; Width for
// Create/Resize.
type Action struct {
	Type     ActionType
	WindowID string
	PaneID   string
	Width    int
}

// Pane is a sidebar pane's geometry within its window, as Decide needs it.
type Pane struct {
	PaneID string
	Left   int
	Top    int
	Width  int
	Height int
	Active bool // pane is the active (and, when zoomed, the shown) pane
}

// WindowState is the per-window snapshot Decide reasons over. Sidebars holds
// only the window's sidebar panes; RealPanes counts the rest.
type WindowState struct {
	WindowID     string
	WindowWidth  int
	WindowHeight int
	Zoomed       bool
	Off          bool // @twm_sidebar_off is set for this window
	RealPanes    int
	Sidebars     []Pane
}

// DesiredWidth is the sidebar width for a window: min(cfg.Width, half the
// window). A value below minSidebarWidth means the window is too narrow for a
// sidebar.
func DesiredWidth(cfg config.Sidebar, windowWidth int) int {
	w := cfg.Width
	if half := windowWidth / 2; w > half {
		w = half
	}
	return w
}

// Decide returns the idempotent set of actions that reconcile one window's
// sidebar state. The rule order checks before it acts so a mutation-triggered
// layout/resize hook cannot start a kill/recreate loop:
//
//  1. zoomed with an active sidebar   → Unzoom (a sidebar must never stay zoomed)
//  2. zoomed (real pane)              → no-op  (the sidebar hides naturally)
//  3. disabled or @twm_sidebar_off    → kill every sidebar
//  4. no real panes, sidebar remains  → kill the window (never orphan a sidebar)
//  5. two or more sidebars            → keep one, kill the rest
//  6. none and wide enough            → Create
//  7. wrong position                  → Kill + Create (the pane is stateless)
//  8. wrong width only                → Resize
func Decide(w WindowState, cfg config.Sidebar, enabled bool) []Action {
	desired := DesiredWidth(cfg, w.WindowWidth)

	if w.Zoomed {
		for _, p := range w.Sidebars {
			if p.Active {
				return []Action{{Type: ActionUnzoom, PaneID: p.PaneID}}
			}
		}
		return nil
	}

	if !enabled || w.Off {
		return killAll(w.Sidebars)
	}

	if w.RealPanes == 0 {
		// Only reap the window when a sidebar is what is being orphaned; a window
		// with no panes at all cannot arise through the CLI, so do nothing.
		if len(w.Sidebars) > 0 {
			return []Action{{Type: ActionKillWindow, WindowID: w.WindowID}}
		}
		return nil
	}

	if len(w.Sidebars) >= 2 {
		keep := keepIndex(w.Sidebars, w, desired)
		var actions []Action
		for i, p := range w.Sidebars {
			if i == keep {
				continue
			}
			actions = append(actions, Action{Type: ActionKill, PaneID: p.PaneID})
		}
		return actions
	}

	if len(w.Sidebars) == 0 {
		if desired >= minSidebarWidth {
			return []Action{{Type: ActionCreate, WindowID: w.WindowID, Width: desired}}
		}
		return nil
	}

	p := w.Sidebars[0]
	if !correctPosition(p, w) {
		actions := []Action{{Type: ActionKill, PaneID: p.PaneID}}
		if desired >= minSidebarWidth {
			actions = append(actions, Action{Type: ActionCreate, WindowID: w.WindowID, Width: desired})
		}
		return actions
	}
	if p.Width != desired {
		if desired >= minSidebarWidth {
			return []Action{{Type: ActionResize, PaneID: p.PaneID, Width: desired}}
		}
		return []Action{{Type: ActionKill, PaneID: p.PaneID}}
	}
	return nil
}

func killAll(panes []Pane) []Action {
	if len(panes) == 0 {
		return nil
	}
	actions := make([]Action, 0, len(panes))
	for _, p := range panes {
		actions = append(actions, Action{Type: ActionKill, PaneID: p.PaneID})
	}
	return actions
}

// correctPosition reports whether a sidebar pane is docked full-height on the
// left edge of its window.
func correctPosition(p Pane, w WindowState) bool {
	return p.Left == 0 && p.Top == 0 && p.Height == w.WindowHeight
}

// keepIndex picks which of several sidebars to keep: the first (by lowest pane
// id) with correct geometry, else the lowest pane id overall.
func keepIndex(panes []Pane, w WindowState, desired int) int {
	best := -1
	for i, p := range panes {
		if !correctPosition(p, w) || p.Width != desired {
			continue
		}
		if best == -1 || paneIDLess(p.PaneID, panes[best].PaneID) {
			best = i
		}
	}
	if best != -1 {
		return best
	}
	best = 0
	for i := 1; i < len(panes); i++ {
		if paneIDLess(panes[i].PaneID, panes[best].PaneID) {
			best = i
		}
	}
	return best
}

// paneIDLess orders canonical pane ids ("%7") by their numeric part.
func paneIDLess(a, b string) bool {
	return paneIDNum(a) < paneIDNum(b)
}

func paneIDNum(id string) int {
	n, err := strconv.Atoi(strings.TrimPrefix(id, "%"))
	if err != nil {
		return 1 << 30
	}
	return n
}
