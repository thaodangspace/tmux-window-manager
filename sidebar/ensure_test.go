package sidebar

import (
	"reflect"
	"testing"

	"github.com/thaodangspace/tmux-window-manager/config"
)

func TestDecide(t *testing.T) {
	cfg := config.Sidebar{Enabled: true, Width: 32, RefreshMS: 1000}

	// A wide window: half = 100, so desired = min(32, 100) = 32.
	const winID = "@3"
	const ww, wh = 200, 50

	correct := Pane{PaneID: "%1", Left: 0, Top: 0, Width: 32, Height: wh}

	base := WindowState{WindowID: winID, WindowWidth: ww, WindowHeight: wh, RealPanes: 1}

	withSidebars := func(w WindowState, ps ...Pane) WindowState {
		w.Sidebars = ps
		return w
	}

	tests := []struct {
		name    string
		state   WindowState
		enabled bool
		want    []Action
	}{
		{
			name:    "correct state is a no-op",
			state:   withSidebars(base, correct),
			enabled: true,
			want:    nil,
		},
		{
			name:    "none and wide enough creates",
			state:   base,
			enabled: true,
			want:    []Action{{Type: ActionCreate, WindowID: winID, Width: 32}},
		},
		{
			name:    "narrow window makes no sidebar",
			state:   WindowState{WindowID: winID, WindowWidth: 18, WindowHeight: wh, RealPanes: 1},
			enabled: true,
			want:    nil,
		},
		{
			name:    "wrong width resizes",
			state:   withSidebars(base, Pane{PaneID: "%1", Left: 0, Top: 0, Width: 20, Height: wh}),
			enabled: true,
			want:    []Action{{Type: ActionResize, PaneID: "%1", Width: 32}},
		},
		{
			name:    "wrong position re-docks",
			state:   withSidebars(base, Pane{PaneID: "%1", Left: 10, Top: 0, Width: 32, Height: wh}),
			enabled: true,
			want:    []Action{{Type: ActionKill, PaneID: "%1"}, {Type: ActionCreate, WindowID: winID, Width: 32}},
		},
		{
			name:    "tiled geometry re-docks",
			state:   withSidebars(base, Pane{PaneID: "%1", Left: 0, Top: 0, Width: 32, Height: 25}),
			enabled: true,
			want:    []Action{{Type: ActionKill, PaneID: "%1"}, {Type: ActionCreate, WindowID: winID, Width: 32}},
		},
		{
			name:    "user split -hbf left of sidebar re-docks",
			state:   withSidebars(WindowState{WindowID: winID, WindowWidth: ww, WindowHeight: wh, RealPanes: 2}, Pane{PaneID: "%1", Left: 40, Top: 0, Width: 32, Height: wh}),
			enabled: true,
			want:    []Action{{Type: ActionKill, PaneID: "%1"}, {Type: ActionCreate, WindowID: winID, Width: 32}},
		},
		{
			name:    "two sidebars keep correct kill rest",
			state:   withSidebars(base, correct, Pane{PaneID: "%2", Left: 40, Top: 0, Width: 32, Height: wh}),
			enabled: true,
			want:    []Action{{Type: ActionKill, PaneID: "%2"}},
		},
		{
			name:    "two wrong sidebars keep lowest id",
			state:   withSidebars(base, Pane{PaneID: "%5", Left: 40, Top: 0, Width: 32, Height: wh}, Pane{PaneID: "%2", Left: 40, Top: 0, Width: 32, Height: wh}),
			enabled: true,
			want:    []Action{{Type: ActionKill, PaneID: "%5"}},
		},
		{
			name:    "disabled kills all sidebars",
			state:   withSidebars(base, correct),
			enabled: false,
			want:    []Action{{Type: ActionKill, PaneID: "%1"}},
		},
		{
			name:    "per-window off kills all sidebars",
			state:   withSidebars(WindowState{WindowID: winID, WindowWidth: ww, WindowHeight: wh, RealPanes: 1, Off: true}, correct),
			enabled: true,
			want:    []Action{{Type: ActionKill, PaneID: "%1"}},
		},
		{
			name:    "no real panes kills the window",
			state:   withSidebars(WindowState{WindowID: winID, WindowWidth: ww, WindowHeight: wh, RealPanes: 0}, correct),
			enabled: true,
			want:    []Action{{Type: ActionKillWindow, WindowID: winID}},
		},
		{
			name:    "zoomed sidebar is unzoomed",
			state:   withSidebars(WindowState{WindowID: winID, WindowWidth: ww, WindowHeight: wh, RealPanes: 1, Zoomed: true}, Pane{PaneID: "%1", Left: 0, Top: 0, Width: 32, Height: wh, Active: true}),
			enabled: true,
			want:    []Action{{Type: ActionUnzoom, PaneID: "%1"}},
		},
		{
			name:    "zoomed real pane is left alone",
			state:   withSidebars(WindowState{WindowID: winID, WindowWidth: ww, WindowHeight: wh, RealPanes: 1, Zoomed: true}, correct),
			enabled: true,
			want:    nil,
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

func TestDesiredWidth(t *testing.T) {
	cfg := config.Sidebar{Width: 32}
	if got := DesiredWidth(cfg, 200); got != 32 {
		t.Fatalf("DesiredWidth wide = %d, want 32", got)
	}
	if got := DesiredWidth(cfg, 40); got != 20 {
		t.Fatalf("DesiredWidth half-cap = %d, want 20", got)
	}
	if got := DesiredWidth(cfg, 18); got != 9 {
		t.Fatalf("DesiredWidth narrow = %d, want 9", got)
	}
}
