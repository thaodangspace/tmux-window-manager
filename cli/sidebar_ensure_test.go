package cli

import (
	"reflect"
	"testing"

	"github.com/thaodangspace/tmux-window-manager/sidebar"
	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
)

// realPane and sidePane build tmuxcli.SidebarPane fixtures. sidePane sets a start
// command that IsSidebarStartCommand recognises so the Sidebar flag is true.
func realPane(win, pane string, ww, wh int) tmuxcli.SidebarPane {
	return tmuxcli.SidebarPane{
		WindowID: win, PaneID: pane,
		WindowWidth: ww, WindowHeight: wh,
	}
}

func sidePane(win, pane string, left, top, w, h, ww, wh int, active bool) tmuxcli.SidebarPane {
	return tmuxcli.SidebarPane{
		WindowID: win, PaneID: pane,
		Left: left, Top: top, Width: w, Height: h,
		WindowWidth: ww, WindowHeight: wh,
		PaneActive:   active,
		StartCommand: tmuxcli.SidebarStartCommand("/opt/twm"),
		Sidebar:      true,
	}
}

func TestWindowStatesGroupingAndCounts(t *testing.T) {
	panes := []tmuxcli.SidebarPane{
		realPane("@1", "%0", 200, 50),
		sidePane("@1", "%1", 0, 0, 32, 50, 200, 50, false),
		realPane("@1", "%2", 200, 50),
		realPane("@2", "%3", 120, 40),
	}

	got := windowStates(panes, "")
	if len(got) != 2 {
		t.Fatalf("got %d windows, want 2", len(got))
	}

	w1 := got[0].state
	if w1.WindowID != "@1" {
		t.Fatalf("first window = %q, want @1", w1.WindowID)
	}
	if w1.RealPanes != 2 {
		t.Fatalf("@1 RealPanes = %d, want 2", w1.RealPanes)
	}
	if len(w1.Sidebars) != 1 {
		t.Fatalf("@1 Sidebars = %d, want 1", len(w1.Sidebars))
	}
	wantSidebar := sidebar.Pane{PaneID: "%1", Left: 0, Top: 0, Width: 32, Height: 50}
	if !reflect.DeepEqual(w1.Sidebars[0], wantSidebar) {
		t.Fatalf("@1 sidebar pane = %#v, want %#v", w1.Sidebars[0], wantSidebar)
	}
	if w1.WindowWidth != 200 || w1.WindowHeight != 50 {
		t.Fatalf("@1 window size = %dx%d, want 200x50", w1.WindowWidth, w1.WindowHeight)
	}

	w2 := got[1].state
	if w2.WindowID != "@2" {
		t.Fatalf("second window = %q, want @2", w2.WindowID)
	}
	if w2.RealPanes != 1 || len(w2.Sidebars) != 0 {
		t.Fatalf("@2 RealPanes=%d Sidebars=%d, want 1/0", w2.RealPanes, len(w2.Sidebars))
	}
}

func TestWindowStatesActiveAndOptions(t *testing.T) {
	p := sidePane("@1", "%1", 0, 0, 32, 50, 200, 50, true)
	p.WindowZoomed = true
	p.SidebarOff = "1"
	p.SidebarEnabled = "0"
	got := windowStates([]tmuxcli.SidebarPane{p, realPane("@1", "%0", 200, 50)}, "")
	if len(got) != 1 {
		t.Fatalf("got %d windows, want 1", len(got))
	}
	st := got[0].state
	if !st.Zoomed {
		t.Fatal("Zoomed = false, want true")
	}
	if !st.Off {
		t.Fatal("Off = false, want true (@twm_sidebar_off=1)")
	}
	if !st.Sidebars[0].Active {
		t.Fatal("sidebar pane Active = false, want true")
	}
	if got[0].runtimeEnabled != "0" {
		t.Fatalf("runtimeEnabled = %q, want 0", got[0].runtimeEnabled)
	}
}

func TestWindowStatesTargetFilter(t *testing.T) {
	panes := []tmuxcli.SidebarPane{
		realPane("@1", "%0", 200, 50),
		realPane("@2", "%1", 200, 50),
		realPane("@3", "%2", 200, 50),
	}
	got := windowStates(panes, "@2")
	if len(got) != 1 {
		t.Fatalf("got %d windows, want 1", len(got))
	}
	if got[0].state.WindowID != "@2" {
		t.Fatalf("filtered window = %q, want @2", got[0].state.WindowID)
	}
}

func TestWindowStatesFirstSeenOrder(t *testing.T) {
	// @2 appears before @1; interleaved rows must not reorder the windows.
	panes := []tmuxcli.SidebarPane{
		realPane("@2", "%0", 200, 50),
		realPane("@1", "%1", 200, 50),
		realPane("@2", "%2", 200, 50),
	}
	got := windowStates(panes, "")
	if len(got) != 2 {
		t.Fatalf("got %d windows, want 2", len(got))
	}
	if got[0].state.WindowID != "@2" || got[1].state.WindowID != "@1" {
		t.Fatalf("order = [%s %s], want [@2 @1]", got[0].state.WindowID, got[1].state.WindowID)
	}
	if got[0].state.RealPanes != 2 {
		t.Fatalf("@2 RealPanes = %d, want 2", got[0].state.RealPanes)
	}
}
