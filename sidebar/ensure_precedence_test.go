package sidebar

import (
	"reflect"
	"testing"

	"github.com/thaodangspace/tmux-window-manager/config"
)

// TestDecidePrecedence pins the Decide rule ORDER for states where more than one
// rule could apply at once. The plan fixes the order (zoomed → disabled/off → no
// real panes → ≥2 sidebars → create → reposition → resize); these cases prove the
// earlier rule wins.
func TestDecidePrecedence(t *testing.T) {
	cfg := config.Sidebar{Enabled: true, Width: 32, RefreshMS: 1000}
	const winID = "@3"
	const ww, wh = 200, 50
	correct := Pane{PaneID: "%1", Left: 0, Top: 0, Width: 32, Height: wh}

	tests := []struct {
		name    string
		state   WindowState
		enabled bool
		want    []Action
	}{
		{
			// zoomed (rule 1) beats disabled (rule 3): a zoomed active sidebar is
			// unzoomed, not killed, so the window is never left zoomed on a sidebar.
			name: "disabled + zoomed active sidebar -> Unzoom (zoomed wins)",
			state: WindowState{
				WindowID: winID, WindowWidth: ww, WindowHeight: wh,
				RealPanes: 1, Zoomed: true,
				Sidebars: []Pane{{PaneID: "%1", Left: 0, Top: 0, Width: 32, Height: wh, Active: true}},
			},
			enabled: false,
			want:    []Action{{Type: ActionUnzoom, PaneID: "%1"}},
		},
		{
			// zoomed (rule 2, real pane shown) beats disabled: no-op while zoomed.
			name: "disabled + zoomed real pane -> no-op (zoomed wins)",
			state: WindowState{
				WindowID: winID, WindowWidth: ww, WindowHeight: wh,
				RealPanes: 1, Zoomed: true,
				Sidebars: []Pane{correct},
			},
			enabled: false,
			want:    nil,
		},
		{
			// disabled (rule 3) beats no-real-panes (rule 4): the sidebar is killed,
			// the window is NOT closed by us.
			name: "disabled + no real panes -> kill sidebar (disabled wins, not KillWindow)",
			state: WindowState{
				WindowID: winID, WindowWidth: ww, WindowHeight: wh,
				RealPanes: 0,
				Sidebars:  []Pane{correct},
			},
			enabled: false,
			want:    []Action{{Type: ActionKill, PaneID: "%1"}},
		},
		{
			// per-window off + no real panes -> off wins the same way.
			name: "off + no real panes -> kill sidebar (off wins)",
			state: WindowState{
				WindowID: winID, WindowWidth: ww, WindowHeight: wh,
				RealPanes: 0, Off: true,
				Sidebars: []Pane{correct},
			},
			enabled: true,
			want:    []Action{{Type: ActionKill, PaneID: "%1"}},
		},
		{
			// ≥2 sidebars (rule 5) beats wrong-width resize (rule 8): the extra is
			// killed rather than resized, even though the kept one is fine and the
			// extra merely has the wrong width.
			name: "two sidebars, one wrong width -> kill extra (dedup wins over resize)",
			state: WindowState{
				WindowID: winID, WindowWidth: ww, WindowHeight: wh, RealPanes: 1,
				Sidebars: []Pane{
					correct, // %1 correct geometry+width -> kept
					{PaneID: "%2", Left: 0, Top: 0, Width: 20, Height: wh}, // wrong width -> killed, not resized
				},
			},
			enabled: true,
			want:    []Action{{Type: ActionKill, PaneID: "%2"}},
		},
		{
			// A brand-new empty window whose only pane is a real shell must Create a
			// sidebar and must NEVER KillWindow.
			name: "brand-new window with one real shell -> Create (never KillWindow)",
			state: WindowState{
				WindowID: winID, WindowWidth: ww, WindowHeight: wh, RealPanes: 1,
			},
			enabled: true,
			want:    []Action{{Type: ActionCreate, WindowID: winID, Width: 32}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Decide(tt.state, cfg, tt.enabled)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Decide() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestDecideNoRealPanesNoSidebar pins Decide's behaviour for the (currently
// unreachable) state RealPanes==0 with no sidebar. Rule 4 reaps a window only to
// remove an ORPHANED sidebar, so with no sidebar to reap Decide does nothing
// rather than closing a window it did not create.
//
// This cannot occur through the CLI: windowStates only builds a WindowState from
// panes that exist, and a window whose panes are all sidebars has
// len(Sidebars)>=1. The guard is defensive.
func TestDecideNoRealPanesNoSidebar(t *testing.T) {
	cfg := config.Sidebar{Enabled: true, Width: 32, RefreshMS: 1000}
	state := WindowState{WindowID: "@3", WindowWidth: 200, WindowHeight: 50, RealPanes: 0}

	if got := Decide(state, cfg, true); got != nil {
		t.Fatalf("Decide() = %#v, want nil (no sidebar to reap)", got)
	}
}
