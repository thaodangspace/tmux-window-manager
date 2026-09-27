package tmuxcli

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSidebarStartCommand(t *testing.T) {
	for _, tt := range []struct {
		bin  string
		want string
	}{
		{bin: "/tmp/twm", want: "'/tmp/twm' sidebar render"},
		{bin: "/path with space/twm", want: "'/path with space/twm' sidebar render"},
		{bin: "/tmp/o'brien", want: `'/tmp/o'\''brien' sidebar render`},
	} {
		t.Run(tt.bin, func(t *testing.T) {
			if got := SidebarStartCommand(tt.bin); got != tt.want {
				t.Fatalf("SidebarStartCommand(%q) = %q, want %q", tt.bin, got, tt.want)
			}
		})
	}
}

// TestSidebarStartCommandShellSafe proves the single-quoting in
// SidebarStartCommand survives a hostile bin path (embedded single quote,
// spaces, and a $(...) command substitution) when a real POSIX shell tokenizes
// it. tmux's split-window runs the start command through the shell, so the
// security property is: the bin must arrive as exactly one argv element equal to
// the original path, followed by "sidebar" and "render", and the $(...) must
// never execute.
func TestSidebarStartCommandShellSafe(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("sh not available: %v", err)
	}
	dir := t.TempDir()
	pwned := filepath.Join(dir, "pwned")
	bin := "/opt/o'brien tools/$(touch " + pwned + ")/twm"

	cmd := SidebarStartCommand(bin)
	// Emulate the shell tokenization tmux performs: parse the start command
	// into positional parameters and print each on its own line.
	script := "set -- " + cmd + "; printf '%s\\n' \"$@\""
	out, err := exec.Command("sh", "-c", script).Output()
	if err != nil {
		t.Fatalf("sh -c %q failed: %v", script, err)
	}
	got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	want := []string{bin, "sidebar", "render"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("shell tokenized %q as %#v, want %#v", cmd, got, want)
	}
	if _, err := os.Stat(pwned); !os.IsNotExist(err) {
		t.Fatalf("command substitution leaked: %q exists (stat err=%v)", pwned, err)
	}
}

func TestIsSidebarStartCommand(t *testing.T) {
	for _, tt := range []struct {
		name string
		s    string
		want bool
	}{
		{name: "our output", s: SidebarStartCommand("/tmp/twm"), want: true},
		// tmux re-quotes multi-word start commands, wrapping the whole thing in
		// double quotes; the marker must still match (contains, not suffix).
		{name: "tmux requoted", s: `"'/tmp/twm' sidebar render"`, want: true},
		{name: "tmux requoted quoted bin", s: `"'/tmp/o'\''brien' sidebar render"`, want: true},
		{name: "bare requoted", s: `"/tmp/twm sidebar render"`, want: true},
		{name: "unrelated file", s: "vim sidebar.go", want: false},
		{name: "prefix only", s: "/tmp/twm sidebar", want: false},
		{name: "empty", s: "", want: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSidebarStartCommand(tt.s); got != tt.want {
				t.Fatalf("IsSidebarStartCommand(%q) = %v, want %v", tt.s, got, tt.want)
			}
		})
	}
}

func TestNotSidebarFilter(t *testing.T) {
	const want = "#{?#{m:* sidebar render*,#{pane_start_command}},0,1}"
	if NotSidebarFilter != want {
		t.Fatalf("NotSidebarFilter = %q, want %q", NotSidebarFilter, want)
	}
}

func TestParseSidebarPanes(t *testing.T) {
	fields := func(f ...string) string { return strings.Join(f, sep) }
	// Field order (render/read path): window_index, window_id, pane_id, pane_pid,
	// window_active, session_attached, window_zoomed_flag, pane_active, sidebar
	// marker, off token, enabled token, session_name, pane_current_path (LAST).
	real := fields(
		"1", "@1", "%1", "1234", "1", "1", "0", "1", "0", "0", "1", "work", "/home/me/my project",
	)
	side := fields(
		"1", "@1", "%2", "5678", "1", "0", "0", "0", "1", "0", "1", "work", "/home/me",
	)
	// pane_current_path is the last field, so an embedded tab folds into Path via
	// SplitN and cannot shift the earlier, validated columns.
	tabbed := fields(
		"2", "@1", "%3", "4321", "1", "1", "0", "0", "0", "0", "", "work", "/home/me/tab",
	) + sep + "extra"
	// A row with a non-canonical window id is rejected (id validation).
	badID := fields(
		"1", "@1;rm", "%4", "4444", "1", "1", "0", "0", "0", "0", "1", "work", "/home/me",
	)
	out := real + "\n" + side + "\n" + tabbed + "\n" + badID + "\n" +
		"truncated\tline\n" + "\n"

	got := parseSidebarPanes(out)
	want := []SidebarPane{
		{
			WindowIndex: 1, WindowID: "@1", PaneID: "%1", PanePID: "1234",
			WindowActive: true, SessionAttached: true, WindowZoomed: false,
			PaneActive: true, Sidebar: false, SidebarOff: "0", SidebarEnabled: "1",
			Session: "work", Path: "/home/me/my project",
		},
		{
			WindowIndex: 1, WindowID: "@1", PaneID: "%2", PanePID: "5678",
			WindowActive: true, SessionAttached: false, WindowZoomed: false,
			PaneActive: false, Sidebar: true, SidebarOff: "0", SidebarEnabled: "1",
			Session: "work", Path: "/home/me",
		},
		{
			WindowIndex: 2, WindowID: "@1", PaneID: "%3", PanePID: "4321",
			WindowActive: true, SessionAttached: true, WindowZoomed: false,
			PaneActive: false, Sidebar: false, SidebarOff: "0", SidebarEnabled: "",
			Session: "work", Path: "/home/me/tab\textra",
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseSidebarPanes() = %#v, want %#v", got, want)
	}
}

// TestParseSidebarPanesStrictFieldCount asserts the render-path parser accepts
// exactly sidebarPaneFields fields and rejects a short line. A line with MORE
// than sidebarPaneFields tabs is intentionally folded into the final
// pane_current_path field (see TestParseSidebarPanes "tabbed" case), so the only
// count rejection is the lower bound.
func TestParseSidebarPanesStrictFieldCount(t *testing.T) {
	join := func(f ...string) string { return strings.Join(f, sep) }
	// Render order: window_index, window_id, pane_id, pane_pid, window_active,
	// session_attached, window_zoomed_flag, pane_active, marker, off, enabled,
	// session_name, pane_current_path.
	base := []string{"1", "@1", "%1", "1234", "1", "1", "0", "1", "0", "0", "1", "work", "/home/me"}

	if got := parseSidebarPanes(join(base...)); len(got) != 1 {
		t.Fatalf("exact %d fields: got %d rows, want 1", sidebarPaneFields, len(got))
	}
	if got := parseSidebarPanes(join(base[:12]...)); len(got) != 0 {
		t.Fatalf("12 fields: got %d rows, want 0", len(got))
	}
	badWinIdx := append([]string(nil), base...)
	badWinIdx[0] = "x"
	if got := parseSidebarPanes(join(badWinIdx...)); len(got) != 0 {
		t.Fatalf("non-numeric window_index: got %d rows, want 0", len(got))
	}
	badPID := append([]string(nil), base...)
	badPID[3] = "notapid"
	if got := parseSidebarPanes(join(badPID...)); len(got) != 0 {
		t.Fatalf("non-numeric pane_pid: got %d rows, want 0", len(got))
	}
}

// TestParseSidebarPanesRejectsForgery is the render-path counterpart to
// TestParseSidebarGeometryRejectsForgery. The render listing DOES carry a
// free-form field (pane_current_path), and tmux emits it raw (a directory name's
// embedded newline is NOT escaped — verified live against tmux 3.5a). So a
// hostile path splits into extra lines in the raw output. This asserts that the
// realistic attack artifacts those extra lines produce — the old 19-field panes
// format row claiming a victim %id, and a bare path tail — fail the field-count /
// id / numeric checks and never enter the parsed set, leaving only genuine rows.
// (The destructive kill/resize paths use ListSidebarGeometry, which carries no
// free-form field at all; this parser feeds only read-only rendering.)
func TestParseSidebarPanesRejectsForgery(t *testing.T) {
	join := func(f ...string) string { return strings.Join(f, sep) }
	good := join("1", "@1", "%1", "1234", "1", "1", "0", "1", "0", "0", "1", "work", "/home/me/evilbase")
	// The genuine row's path ends in a newline in the raw tmux output; everything
	// after it arrives as separate lines. First: the pre-hardening 19-field panes
	// row an attacker would inject, claiming the foreign pane %666 as a sidebar.
	// SplitN folds its surplus tabs into the last field, but field 0 ("work") is a
	// non-numeric window_index, so it is dropped.
	forged19 := join("work", "2", "@2", "%666", "999", "/evil", "'x' sidebar render",
		"0", "0", "32", "24", "200", "50", "1", "1", "0", "0", "", "1")
	pathTail := "/home/me/evil"
	out := good + "\n" + forged19 + "\n" + pathTail + "\n" + "truncated\tline\n" + "\n"

	got := parseSidebarPanes(out)
	if len(got) != 1 {
		t.Fatalf("parseSidebarPanes() = %d rows, want 1 (only the genuine line): %#v", len(got), got)
	}
	if got[0].PaneID != "%1" {
		t.Fatalf("surviving row pane id = %q, want %%1", got[0].PaneID)
	}
	for _, p := range got {
		if p.PaneID == "%666" {
			t.Fatal("forged foreign pane id %666 entered the render set")
		}
	}
}

// TestParseSidebarGeometryRejectsForgery is the security regression for the
// destructive listing: a directory name containing a newline plus tab-separated
// fields (the classic pane_current_path row-forgery) must not produce a
// SidebarPane. The geometry listing emits no free-form field, so the forged blob
// only ever arrives as lines that fail the exact-count / id / numeric checks.
func TestParseSidebarGeometryRejectsForgery(t *testing.T) {
	join := func(f ...string) string { return strings.Join(f, sep) }
	// Geometry order: window_id, pane_id, left, top, width, height, window_width,
	// window_height, zoomed, pane_active, marker, off, enabled.
	good := join("@1", "%1", "0", "0", "32", "24", "200", "50", "0", "0", "1", "0", "1")
	// What a hostile directory name would have injected under a free-form listing:
	// a whole extra row carrying a REAL foreign pane id and a forged sidebar
	// marker. Here it is fed to the parser directly as the tail of a split path.
	forged19 := join("work", "2", "@2", "%666", "999", "/evil", "'x' sidebar render",
		"0", "0", "32", "24", "200", "50", "1", "1", "0", "0", "", "1")
	pathTail := "/home/me/evil"
	out := good + "\n" + forged19 + "\n" + pathTail + "\n" + "truncated\tline\n" + "\n"

	got := parseSidebarGeometry(out)
	if len(got) != 1 {
		t.Fatalf("parseSidebarGeometry() = %d rows, want 1 (only the genuine line): %#v", len(got), got)
	}
	if got[0].PaneID != "%1" {
		t.Fatalf("surviving row pane id = %q, want %%1", got[0].PaneID)
	}
	for _, p := range got {
		if p.PaneID == "%666" {
			t.Fatal("forged foreign pane id %666 entered the geometry kill set")
		}
	}
}

// TestParseSidebarGeometryStrictFieldCount asserts the parser accepts exactly
// sidebarGeometryFields fields and rejects any other count or malformed field.
func TestParseSidebarGeometryStrictFieldCount(t *testing.T) {
	join := func(f ...string) string { return strings.Join(f, sep) }
	base := []string{"@1", "%1", "0", "0", "32", "24", "200", "50", "0", "0", "1", "0", "1"}
	dup := func() []string { return append([]string(nil), base...) }

	if got := parseSidebarGeometry(join(base...)); len(got) != 1 {
		t.Fatalf("exact %d fields: got %d rows, want 1", sidebarGeometryFields, len(got))
	}
	if got := parseSidebarGeometry(join(base[:12]...)); len(got) != 0 {
		t.Fatalf("12 fields: got %d rows, want 0", len(got))
	}
	if got := parseSidebarGeometry(join(append(dup(), "extra")...)); len(got) != 0 {
		t.Fatalf("14 fields: got %d rows, want 0", len(got))
	}
	badID := dup()
	badID[0] = "@1;rm"
	if got := parseSidebarGeometry(join(badID...)); len(got) != 0 {
		t.Fatalf("bad window id: got %d rows, want 0", len(got))
	}
	badNum := dup()
	badNum[2] = "x"
	if got := parseSidebarGeometry(join(badNum...)); len(got) != 0 {
		t.Fatalf("non-numeric geometry: got %d rows, want 0", len(got))
	}
}

func TestValidWindowID(t *testing.T) {
	for _, tt := range []struct {
		value string
		valid bool
	}{
		{value: "@0", valid: true},
		{value: "@31", valid: true},
		{value: "", valid: false},
		{value: "@", valid: false},
		{value: "31", valid: false},
		{value: "%1", valid: false},
		{value: "@1;rm", valid: false},
		{value: "@1 rm", valid: false},
	} {
		t.Run(tt.value, func(t *testing.T) {
			if got := ValidWindowID(tt.value); got != tt.valid {
				t.Fatalf("ValidWindowID(%q) = %v, want %v", tt.value, got, tt.valid)
			}
		})
	}
}

func TestSidebarIDValidationRejectsInjection(t *testing.T) {
	if _, err := SplitSidebarArgs("@1;rm", "/tmp/twm", 32); !errors.Is(err, ErrInvalidWindowID) {
		t.Fatalf("SplitSidebarArgs(@1;rm) err = %v, want ErrInvalidWindowID", err)
	}
	if _, err := KillWindowArgs("@1;rm"); !errors.Is(err, ErrInvalidWindowID) {
		t.Fatalf("KillWindowArgs(@1;rm) err = %v, want ErrInvalidWindowID", err)
	}
	if _, err := SetWindowOptionArgs("@1;rm", "@twm_sidebar_off", "1"); !errors.Is(err, ErrInvalidWindowID) {
		t.Fatalf("SetWindowOptionArgs(@1;rm) err = %v, want ErrInvalidWindowID", err)
	}
	if _, err := KillPaneArgs("%1;rm"); !errors.Is(err, ErrInvalidPaneID) {
		t.Fatalf("KillPaneArgs(%%1;rm) err = %v, want ErrInvalidPaneID", err)
	}
	if _, err := ResizePaneXArgs("%1 rm", 20); !errors.Is(err, ErrInvalidPaneID) {
		t.Fatalf("ResizePaneXArgs(%%1 rm) err = %v, want ErrInvalidPaneID", err)
	}
}

func TestSplitSidebarArgs(t *testing.T) {
	got, err := SplitSidebarArgs("@3", "/tmp/twm", 32)
	if err != nil {
		t.Fatalf("SplitSidebarArgs() error = %v", err)
	}
	want := []string{
		"split-window", "-hbf", "-d",
		"-t", "@3",
		"-l", "32",
		"-P", "-F", "#{pane_id}",
		"'/tmp/twm' sidebar render",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SplitSidebarArgs() = %#v, want %#v", got, want)
	}
	if _, err := SplitSidebarArgs("@3", "/tmp/twm", 0); err == nil {
		t.Fatal("SplitSidebarArgs() with zero width: want error")
	}
}

func TestKillResizeUnzoomArgs(t *testing.T) {
	for _, tt := range []struct {
		name string
		got  func() ([]string, error)
		want []string
	}{
		{"kill-pane", func() ([]string, error) { return KillPaneArgs("%7") }, []string{"kill-pane", "-t", "%7"}},
		{"kill-window", func() ([]string, error) { return KillWindowArgs("@4") }, []string{"kill-window", "-t", "@4"}},
		{"resize-pane", func() ([]string, error) { return ResizePaneXArgs("%7", 32) }, []string{"resize-pane", "-t", "%7", "-x", "32"}},
		{"unzoom", func() ([]string, error) { return UnzoomPaneArgs("%7") }, []string{"resize-pane", "-t", "%7", "-Z"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.got()
			if err != nil {
				t.Fatalf("%s error = %v", tt.name, err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("%s = %#v, want %#v", tt.name, got, tt.want)
			}
		})
	}
	if _, err := ResizePaneXArgs("%7", 0); err == nil {
		t.Fatal("ResizePaneXArgs() with zero width: want error")
	}
}

func TestOptionArgs(t *testing.T) {
	if got, err := SetGlobalOptionArgs("@twm_sidebar_enabled", "1"); err != nil ||
		!reflect.DeepEqual(got, []string{"set-option", "-g", "@twm_sidebar_enabled", "1"}) {
		t.Fatalf("SetGlobalOptionArgs() = %#v, %v", got, err)
	}
	if got, err := UnsetGlobalOptionArgs("@twm_sidebar_enabled"); err != nil ||
		!reflect.DeepEqual(got, []string{"set-option", "-gu", "@twm_sidebar_enabled"}) {
		t.Fatalf("UnsetGlobalOptionArgs() = %#v, %v", got, err)
	}
	if got, err := SetWindowOptionArgs("@4", "@twm_sidebar_off", "1"); err != nil ||
		!reflect.DeepEqual(got, []string{"set-option", "-w", "-t", "@4", "@twm_sidebar_off", "1"}) {
		t.Fatalf("SetWindowOptionArgs() = %#v, %v", got, err)
	}
	for _, name := range []string{"", "bad name", "@twm;rm", "-flag"} {
		if _, err := SetGlobalOptionArgs(name, "1"); !errors.Is(err, ErrInvalidOption) {
			t.Fatalf("SetGlobalOptionArgs(%q) err = %v, want ErrInvalidOption", name, err)
		}
	}
}

func TestHookArgs(t *testing.T) {
	got, err := SetHookArgs("after-new-window", 90, "run-shell -b \"twm sidebar ensure\"")
	if err != nil {
		t.Fatalf("SetHookArgs() error = %v", err)
	}
	want := []string{"set-hook", "-g", "after-new-window[90]", "run-shell -b \"twm sidebar ensure\""}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SetHookArgs() = %#v, want %#v", got, want)
	}

	gotU, err := UnsetHookArgs("after-new-window", 90)
	if err != nil {
		t.Fatalf("UnsetHookArgs() error = %v", err)
	}
	wantU := []string{"set-hook", "-gu", "after-new-window[90]"}
	if !reflect.DeepEqual(gotU, wantU) {
		t.Fatalf("UnsetHookArgs() = %#v, want %#v", gotU, wantU)
	}

	if _, err := SetHookArgs("bad_hook", 90, "cmd"); !errors.Is(err, ErrInvalidHookName) {
		t.Fatalf("SetHookArgs(bad_hook) err = %v, want ErrInvalidHookName", err)
	}
	if _, err := UnsetHookArgs("after-new-window", -1); err == nil {
		t.Fatal("UnsetHookArgs() with negative index: want error")
	}
}
