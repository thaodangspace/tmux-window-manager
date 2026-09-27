package cli

import (
	"sort"
	"strings"
	"testing"

	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
)

// wantHookNames is the exact set of hook names the sidebar installs at
// sidebarHookIndex. after-break-pane / after-move-window are intentionally
// excluded (they do not exist on tmux).
var wantHookNames = []string{
	"after-new-window",
	"session-created",
	"after-split-window",
	"window-linked",
	"window-unlinked",
	"pane-exited",
	"after-kill-pane",
	"window-layout-changed",
	"window-resized",
	"client-resized",
	"window-pane-changed",
	"session-window-changed",
	"client-session-changed",
	"after-select-window",
}

// hookByName indexes a hook table for golden comparison.
func hookByName(hooks []sidebarHook) map[string]string {
	m := make(map[string]string, len(hooks))
	for _, h := range hooks {
		m[h.Name] = h.Cmd
	}
	return m
}

func TestSidebarHookIndexIs90(t *testing.T) {
	if sidebarHookIndex != 90 {
		t.Fatalf("sidebarHookIndex = %d, want 90", sidebarHookIndex)
	}
}

func TestSidebarHookNamesExact(t *testing.T) {
	got := sidebarHookNames()
	if len(got) != len(wantHookNames) {
		t.Fatalf("got %d hook names, want %d: %v", len(got), len(wantHookNames), got)
	}
	gs := append([]string(nil), got...)
	ws := append([]string(nil), wantHookNames...)
	sort.Strings(gs)
	sort.Strings(ws)
	for i := range ws {
		if gs[i] != ws[i] {
			t.Fatalf("hook names = %v, want %v", gs, ws)
		}
	}
	// The nonexistent hooks must never appear.
	for _, bad := range []string{"after-break-pane", "after-move-window"} {
		for _, n := range got {
			if n == bad {
				t.Fatalf("hook table contains nonexistent hook %q", bad)
			}
		}
	}
	// Every name must be a valid tmux hook name at the fixed index.
	for _, n := range got {
		if _, err := tmuxcli.SetHookArgs(n, sidebarHookIndex, "x"); err != nil {
			t.Fatalf("SetHookArgs(%q, %d) error: %v", n, sidebarHookIndex, err)
		}
	}
}

func TestSidebarHookTableGolden(t *testing.T) {
	const bin = "/opt/twm"
	m := hookByName(sidebarHooks(bin, 32))
	if len(m) != len(wantHookNames) {
		t.Fatalf("table has %d unique names, want %d", len(m), len(wantHookNames))
	}

	wantEnsure := `run-shell -b "'/opt/twm' sidebar ensure -t #{window_id}"`
	for _, n := range sidebarEnsureHookNames {
		if m[n] != wantEnsure {
			t.Errorf("hook %q = %q, want %q", n, m[n], wantEnsure)
		}
	}

	wantGuard := `#{?#{==:#{P:#{?#{m:* sidebar render*,#{pane_start_command}},` +
		`#{?#{==:#{pane_left},0},#{?#{==:#{pane_height},#{window_height}},` +
		`#{?#{==:#{pane_width},32},g,b},b},b},}},g},0,1}`
	wantGuarded := `if -F '` + wantGuard + `' { ` + wantEnsure + ` }`
	for _, n := range sidebarGuardedEnsureHookNames {
		if m[n] != wantGuarded {
			t.Errorf("guarded hook %q =\n  %q\nwant\n  %q", n, m[n], wantGuarded)
		}
	}

	wantBounce := `if -F '#{m:* sidebar render*,#{pane_start_command}}' 'select-pane -R'`
	if m["window-pane-changed"] != wantBounce {
		t.Errorf("bounce hook = %q, want %q", m["window-pane-changed"], wantBounce)
	}

	wantWake := `run-shell -b "kill -USR1 #{P:#{?#{m:* sidebar render*,#{pane_start_command}},#{pane_pid} ,}} 2>/dev/null; true"`
	for _, n := range sidebarWakeHookNames {
		if m[n] != wantWake {
			t.Errorf("wake hook %q = %q, want %q", n, m[n], wantWake)
		}
	}
}

// TestSidebarHookInterpolation is the security check: only #{window_id} reaches
// the ensure shell command and only #{pane_pid} reaches the wake shell command;
// the focus bounce spawns no process at all.
func TestSidebarHookInterpolation(t *testing.T) {
	ensure := sidebarEnsureCmd("/opt/twm")
	if !strings.Contains(ensure, "#{window_id}") {
		t.Errorf("ensure cmd missing #{window_id}: %q", ensure)
	}
	if n := strings.Count(ensure, "#{"); n != 1 {
		t.Errorf("ensure cmd has %d interpolations, want exactly 1 (#{window_id}): %q", n, ensure)
	}

	wake := sidebarWakeCmd()
	if !strings.Contains(wake, "#{pane_pid}") {
		t.Errorf("wake cmd missing #{pane_pid}: %q", wake)
	}
	if strings.Contains(wake, "#{window_id}") {
		t.Errorf("wake cmd must not interpolate #{window_id}: %q", wake)
	}
	// Only pane identity that reaches the kill shell is the numeric pid.
	for _, bad := range []string{"#{pane_current_path}", "#{session_name}", "#{client_name}"} {
		if strings.Contains(wake, bad) {
			t.Errorf("wake cmd leaks %s into the shell: %q", bad, wake)
		}
	}

	bounce := sidebarBounceCmd()
	if strings.Contains(bounce, "run-shell") {
		t.Errorf("focus bounce must not spawn a process: %q", bounce)
	}
	if !strings.HasPrefix(bounce, "if -F ") || !strings.Contains(bounce, "select-pane -R") {
		t.Errorf("focus bounce shape unexpected: %q", bounce)
	}
}

// TestSidebarBinQuoting confirms a hostile binary path is single-quoted (with
// embedded quotes escaped) exactly like the sidebar start command, so it cannot
// break out of the run-shell argument.
func TestSidebarBinQuoting(t *testing.T) {
	bin := "/tmp/a b/tw'm"
	ensure := sidebarEnsureCmd(bin)
	wantQuoted := tmuxcli.QuoteBin(bin)
	if wantQuoted != `'/tmp/a b/tw'\''m'` {
		t.Fatalf("QuoteBin = %q, want the POSIX escaped form", wantQuoted)
	}
	if !strings.Contains(ensure, wantQuoted) {
		t.Errorf("ensure cmd %q does not contain quoted bin %q", ensure, wantQuoted)
	}
	// The quoting must match SidebarStartCommand's, keeping one source of truth.
	if !strings.HasPrefix(tmuxcli.SidebarStartCommand(bin), wantQuoted) {
		t.Errorf("SidebarStartCommand quoting diverged from QuoteBin")
	}
}

func TestSidebarGeometryGuardWidth(t *testing.T) {
	g32 := sidebarGeometryGuard(32)
	g48 := sidebarGeometryGuard(48)
	if g32 == g48 {
		t.Fatal("guard did not vary with width")
	}
	if !strings.Contains(g32, "#{==:#{pane_width},32}") {
		t.Errorf("width-32 guard missing width comparison: %q", g32)
	}
	if !strings.Contains(g48, "#{==:#{pane_width},48}") {
		t.Errorf("width-48 guard missing width comparison: %q", g48)
	}
	// The guard checks position, height and width and keys off the sidebar marker.
	for _, want := range []string{
		"#{==:#{pane_left},0}",
		"#{==:#{pane_height},#{window_height}}",
		"#{m:* sidebar render*,#{pane_start_command}}",
	} {
		if !strings.Contains(g32, want) {
			t.Errorf("guard missing %q: %q", want, g32)
		}
	}
	// Result is 0 (skip) when exactly one correct sidebar ("g"), else 1 (run).
	if !strings.HasPrefix(g32, "#{?#{==:") || !strings.HasSuffix(g32, ",g},0,1}") {
		t.Errorf("guard result shape unexpected: %q", g32)
	}
}

// TestSidebarUninstallMirrorsInstall checks the uninstall unset args mirror the
// install set args: same names, same index, one unset per installed hook.
func TestSidebarUninstallMirrorsInstall(t *testing.T) {
	installed := sidebarHooks("/opt/twm", 32)
	names := sidebarHookNames()
	if len(names) != len(installed) {
		t.Fatalf("names(%d) and hooks(%d) length mismatch", len(names), len(installed))
	}
	inSet := hookByName(installed)
	for _, n := range names {
		if _, ok := inSet[n]; !ok {
			t.Fatalf("uninstall name %q not present in install table", n)
		}
		set, err := tmuxcli.SetHookArgs(n, sidebarHookIndex, inSet[n])
		if err != nil {
			t.Fatalf("SetHookArgs(%q): %v", n, err)
		}
		unset, err := tmuxcli.UnsetHookArgs(n, sidebarHookIndex)
		if err != nil {
			t.Fatalf("UnsetHookArgs(%q): %v", n, err)
		}
		// Both must target the same indexed hook slot name[90].
		if set[2] != unset[2] {
			t.Fatalf("set target %q != unset target %q", set[2], unset[2])
		}
		if !strings.HasSuffix(unset[2], "[90]") {
			t.Fatalf("unset target %q not at index 90", unset[2])
		}
	}
}

// TestSidebarHookCategoriesGolden pins each hook name to its category using
// hardcoded expectations from the spec (Phase 11), independent of the production
// slices. The other tests derive their expectations from those same slices, so a
// name silently moved between categories (e.g. window-resized demoted from the
// geometry-guarded set to the unconditional ensure set, which would spawn a
// process on every resize) would pass them but fail here.
func TestSidebarHookCategoriesGolden(t *testing.T) {
	wantEnsure := []string{
		"after-new-window",
		"session-created",
		"after-split-window",
		"window-linked",
		"window-unlinked",
		"pane-exited",
		"after-kill-pane",
	}
	wantGuarded := []string{
		"window-layout-changed",
		"window-resized",
		"client-resized",
	}
	wantWake := []string{
		"session-window-changed",
		"client-session-changed",
		"after-select-window",
	}
	const wantBounce = "window-pane-changed"

	sameSet := func(name string, got, want []string) {
		t.Helper()
		gs := append([]string(nil), got...)
		ws := append([]string(nil), want...)
		sort.Strings(gs)
		sort.Strings(ws)
		if len(gs) != len(ws) {
			t.Fatalf("%s has %d names %v, want %d %v", name, len(gs), got, len(ws), want)
		}
		for i := range ws {
			if gs[i] != ws[i] {
				t.Fatalf("%s = %v, want %v", name, got, want)
			}
		}
	}
	sameSet("unconditional ensure", sidebarEnsureHookNames, wantEnsure)
	sameSet("guarded ensure", sidebarGuardedEnsureHookNames, wantGuarded)
	sameSet("wake", sidebarWakeHookNames, wantWake)
	if sidebarBounceHookName != wantBounce {
		t.Fatalf("bounce hook = %q, want %q", sidebarBounceHookName, wantBounce)
	}

	// The four categories must partition exactly 14 distinct hook names.
	const wantTotal = 14
	if got := len(sidebarHookNames()); got != wantTotal {
		t.Fatalf("total hook names = %d, want %d", got, wantTotal)
	}
	if n := len(wantEnsure) + len(wantGuarded) + len(wantWake) + 1; n != wantTotal {
		t.Fatalf("category counts sum to %d, want %d", n, wantTotal)
	}

	// A guarded event must never also appear as an unconditional ensure (that
	// would spawn a process on every firing, defeating the geometry guard).
	guarded := map[string]bool{}
	for _, n := range sidebarGuardedEnsureHookNames {
		guarded[n] = true
	}
	for _, n := range sidebarEnsureHookNames {
		if guarded[n] {
			t.Fatalf("hook %q is both unconditional and guarded", n)
		}
	}
}

// TestSidebarGuardedEnsureShellSafety confirms the geometry-guarded hooks (the 3
// high-frequency resize/layout events) only ever spawn the same ensure run-shell
// as the unconditional hooks, and that the tmux-evaluated guard format cannot
// break out of the run-shell's double-quoted argument. The guard interpolates
// pane geometry, but that is evaluated by tmux (never a shell); the only
// interpolation that reaches the spawned shell is #{window_id}.
func TestSidebarGuardedEnsureShellSafety(t *testing.T) {
	const bin = "/opt/twm"
	cmd := sidebarGuardedEnsureCmd(bin, 32)

	if !strings.HasPrefix(cmd, "if -F '") {
		t.Fatalf("guarded cmd must start with an if -F guard: %q", cmd)
	}
	if !strings.HasSuffix(cmd, " }") {
		t.Fatalf("guarded cmd must end with a command group: %q", cmd)
	}

	// The shell-executed body is exactly the plain ensure command: the guard
	// only decides whether to run it, it never rewrites it.
	start := strings.Index(cmd, "{ ")
	end := strings.LastIndex(cmd, " }")
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("could not locate command group in %q", cmd)
	}
	body := cmd[start+2 : end]
	if body != sidebarEnsureCmd(bin) {
		t.Fatalf("guarded body = %q, want plain ensure cmd %q", body, sidebarEnsureCmd(bin))
	}
	if n := strings.Count(body, "#{"); n != 1 || !strings.Contains(body, "#{window_id}") {
		t.Fatalf("guarded shell body must interpolate only #{window_id}: %q", body)
	}

	// The guard format sits between `if -F '` and `' {`; it must be single-quoted
	// with no embedded double quote that could terminate the run-shell string.
	guard := cmd[len("if -F '"):strings.Index(cmd, "' {")]
	if strings.Contains(guard, `"`) {
		t.Fatalf("guard format contains a double quote and could break the run-shell arg: %q", guard)
	}
	if strings.Contains(guard, "run-shell") {
		t.Fatalf("guard format must not itself spawn a process: %q", guard)
	}
}

func TestSidebarToggleFlagParsing(t *testing.T) {
	cmd := newSidebarToggleCommand()
	if err := cmd.Flags().Parse([]string{"-t", "@7"}); err != nil {
		t.Fatalf("parse -t: %v", err)
	}
	got, err := cmd.Flags().GetString("target")
	if err != nil {
		t.Fatalf("GetString(target): %v", err)
	}
	if got != "@7" {
		t.Fatalf("target = %q, want @7", got)
	}
}
