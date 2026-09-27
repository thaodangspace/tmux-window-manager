package sidebar

import (
	"bytes"
	"testing"
	"time"

	"github.com/thaodangspace/tmux-window-manager/config"
	"github.com/thaodangspace/tmux-window-manager/store"
	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
)

// widePanes is testPanes with a known window width so DesiredWidth is
// deterministic in the config-reload tests.
func widePanes() []tmuxcli.SidebarPane {
	p := testPanes()
	for i := range p {
		p[i].WindowWidth = 200
	}
	return p
}

// TestReloadConfigAppliesWidthAndRefresh verifies that a config file change (new
// mtime) resizes the sidebar to the new DesiredWidth and updates the visible tick
// interval (Phase 9 step 5). newLooper seeds the cached config from the first
// Config() call, so only the second, changed read triggers the live apply.
func TestReloadConfigAppliesWidthAndRefresh(t *testing.T) {
	var out bytes.Buffer
	panes := widePanes()

	cfg := config.Sidebar{Enabled: true, Width: 32, RefreshMS: 1000}
	mtime := int64(1)
	var resized []int

	deps := Deps{
		Self:       selfID,
		Home:       "/home/u",
		GitRoot:    func(string) string { return "/home/u/proj" },
		ListPanes:  func() ([]tmuxcli.SidebarPane, error) { return panes, nil },
		Snapshot:   func() string { return psSnap },
		DB:         &fakeDB{dv: 1},
		Config:     func() (config.Sidebar, int64, error) { return cfg, mtime, nil },
		ResizeSelf: func(w int) { resized = append(resized, w) },
		Size:       func() (int, int) { return 32, 20 },
		Out:        &out,
		Refresh:    10 * time.Millisecond,
	}

	l := newLooper(deps)
	if l.refresh != time.Second {
		t.Fatalf("seeded refresh = %v, want 1s", l.refresh)
	}

	// The file changed: wider sidebar, faster refresh, bumped mtime.
	cfg = config.Sidebar{Enabled: true, Width: 40, RefreshMS: 2000}
	mtime = 2
	l.tick()

	wantW := DesiredWidth(cfg, 200) // min(40, 200/2) == 40
	if len(resized) != 1 || resized[0] != wantW {
		t.Fatalf("ResizeSelf calls = %v, want [%d]", resized, wantW)
	}
	if l.refresh != 2*time.Second {
		t.Fatalf("refresh after reload = %v, want 2s", l.refresh)
	}
}

// TestReloadConfigNoopWhenMtimeUnchanged verifies the loop keys the reload off
// the file mtime: an unchanged mtime performs no resize even when the returned
// width differs from the cached one.
func TestReloadConfigNoopWhenMtimeUnchanged(t *testing.T) {
	var out bytes.Buffer
	panes := widePanes()
	var resized []int

	deps := Deps{
		Self:      selfID,
		Home:      "/home/u",
		GitRoot:   func(string) string { return "/home/u/proj" },
		ListPanes: func() ([]tmuxcli.SidebarPane, error) { return panes, nil },
		Snapshot:  func() string { return psSnap },
		DB:        &fakeDB{dv: 1},
		Config: func() (config.Sidebar, int64, error) {
			return config.Sidebar{Enabled: true, Width: 80, RefreshMS: 500}, 5, nil
		},
		ResizeSelf: func(w int) { resized = append(resized, w) },
		Size:       func() (int, int) { return 32, 20 },
		Out:        &out,
		Refresh:    10 * time.Millisecond,
	}

	l := newLooper(deps) // seeds lastMtime=5, cfg width 80
	l.tick()             // same mtime 5 -> reloadConfig returns early
	if len(resized) != 0 {
		t.Fatalf("ResizeSelf called %v on unchanged mtime, want none", resized)
	}
}

// TestTickRefetchesLiveOnDataVersionBump is the positive counterpart to
// TestTickSkipsLiveWhenUnchanged: when a hook writer bumps data_version the loop
// must re-read the status rows even though the pane set is unchanged.
func TestTickRefetchesLiveOnDataVersionBump(t *testing.T) {
	var out bytes.Buffer
	db := &fakeDB{rows: []store.Status{{Agent: "claude", Pid: 2000, Status: store.Idle, UpdatedAt: 1}}, dv: 1}
	l := newLooper(baseDeps(&out, db, nil))

	l.tick()  // Live #1 (first fetch)
	db.dv = 2 // a hook committed a change
	l.tick()  // data_version changed -> Live #2
	if db.liveCalls != 2 {
		t.Fatalf("Live called %d times, want 2 after a data_version bump", db.liveCalls)
	}
}

// TestTickRefetchesLiveOnPaneChange verifies Live is re-read when the pane set
// changes even while data_version is stable (a new pane can host a matching row).
func TestTickRefetchesLiveOnPaneChange(t *testing.T) {
	var out bytes.Buffer
	db := &fakeDB{rows: []store.Status{{Agent: "claude", Pid: 2000, Status: store.Idle, UpdatedAt: 1}}, dv: 9}
	panes := testPanes()
	deps := baseDeps(&out, db, nil)
	deps.ListPanes = func() ([]tmuxcli.SidebarPane, error) { return panes, nil }
	l := newLooper(deps)

	l.tick() // Live #1
	// A new real pane appears; data_version stays 9.
	panes = append(panes, tmuxcli.SidebarPane{
		Session: "work", WindowIndex: 1, WindowID: "@1", PaneID: "%2",
		PanePID: "3000", Path: "/home/u/other",
		WindowActive: true, SessionAttached: true,
	})
	l.tick() // pane set changed -> Live #2
	if db.liveCalls != 2 {
		t.Fatalf("Live called %d times, want 2 after a pane-set change", db.liveCalls)
	}
}
