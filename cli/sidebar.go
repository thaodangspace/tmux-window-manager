package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/tmux-window-manager/config"
	"github.com/thaodangspace/tmux-window-manager/sidebar"
	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
)

// sidebarHookIndex is the fixed global-hook array index the sidebar owns. Using
// one reserved index everywhere means install/uninstall touch only our slot and
// never clobber a user's hooks at other indices (e.g. index 0).
const sidebarHookIndex = 90

// sidebarEnabledOption is the runtime tmux option toggled by enable/disable. It
// takes precedence over the twm.toml [sidebar] enabled value (config.ResolveEnabled).
const sidebarEnabledOption = "@twm_sidebar_enabled"

// sidebarOffOption is the per-window tmux option toggled by `sidebar toggle` to
// hide the sidebar in a single window.
const sidebarOffOption = "@twm_sidebar_off"

// Hook names, grouped by the command they run. Every name is installed at
// sidebarHookIndex. after-break-pane / after-move-window are intentionally
// absent: they do not exist on tmux (break/move are covered by window-linked /
// window-unlinked / window-layout-changed). See docs/plans/sidebar-spike/FINDINGS.md.
var (
	// sidebarEnsureHookNames fire ensure unconditionally: lifecycle events where
	// the pane set changes and geometry is cheap to reconcile.
	sidebarEnsureHookNames = []string{
		"after-new-window",
		"session-created",
		"after-split-window",
		"window-linked",
		"window-unlinked",
		"pane-exited",
		"after-kill-pane",
	}
	// sidebarGuardedEnsureHookNames fire on high-frequency geometry events, so a
	// tmux-only geometry guard skips the process spawn when the sidebar is
	// already docked correctly (see sidebarGeometryGuard).
	sidebarGuardedEnsureHookNames = []string{
		"window-layout-changed",
		"window-resized",
		"client-resized",
	}
	// sidebarWakeHookNames signal visible-sidebar render loops (SIGUSR1) after a
	// focus/session change so a hidden→visible transition redraws promptly.
	sidebarWakeHookNames = []string{
		"session-window-changed",
		"client-session-changed",
		"after-select-window",
	}
)

// sidebarBounceHookName fires the tmux-only focus bounce (no process spawn).
const sidebarBounceHookName = "window-pane-changed"

// sidebarHook is one global hook the plugin installs at sidebarHookIndex.
type sidebarHook struct {
	Name string
	Cmd  string
}

// sidebarEnsureCmd builds the run-shell command that reconciles one window. Only
// #{window_id} is interpolated into the shell, and bin is single-quoted by
// QuoteBin, so a hostile path or window value cannot inject shell words.
func sidebarEnsureCmd(bin string) string {
	return `run-shell -b "` + tmuxcli.QuoteBin(bin) + ` sidebar ensure -t #{window_id}"`
}

// sidebarGeometryGuard returns a tmux `-f`/`if -F` format that is "1" (run
// ensure) unless the window has exactly one correctly docked sidebar pane, and
// "0" (skip) when it does. A sidebar is "correct" when pane_left is 0, its height
// equals the window height, and its width equals the configured width. The
// per-pane token is empty for real panes, "g" for a correct sidebar and "b" for
// a mis-docked one; #{P:…} concatenates them, so exactly one correct sidebar
// yields "g" and anything else (none, extra, or wrong geometry) differs.
//
// The width baked in here is the config width at install time. Narrow windows
// dock at window_width/2 instead, so the guard reads "b" there and ensure runs
// (a harmless no-op); a later config width change is re-baked on the next
// install / TPM re-source. A window with @twm_sidebar_off and a leftover sidebar
// would read "g" and be skipped by these guarded hooks, but `sidebar toggle`
// removes it directly, so this is not reachable in normal use.
func sidebarGeometryGuard(width int) string {
	w := strconv.Itoa(width)
	correct := `#{?#{==:#{pane_left},0},` +
		`#{?#{==:#{pane_height},#{window_height}},` +
		`#{?#{==:#{pane_width},` + w + `},g,b},b},b}`
	pane := `#{?#{m:* sidebar render*,#{pane_start_command}},` + correct + `,}`
	loop := `#{P:` + pane + `}`
	return `#{?#{==:` + loop + `,g},0,1}`
}

// sidebarGuardedEnsureCmd wraps the ensure run-shell in an `if -F` geometry
// guard. tmux `{ … }` command grouping avoids nesting the single-quoted bin
// inside another quoted string.
func sidebarGuardedEnsureCmd(bin string, width int) string {
	return `if -F '` + sidebarGeometryGuard(width) + `' { ` + sidebarEnsureCmd(bin) + ` }`
}

// sidebarBounceCmd is the tmux-only focus bounce: when the newly-active pane is a
// sidebar, select the pane to its right. select-pane -R is used because
// last-pane errors when there is no last pane. No process is spawned.
func sidebarBounceCmd() string {
	return `if -F '#{m:* sidebar render*,#{pane_start_command}}' 'select-pane -R'`
}

// sidebarWakeCmd sends SIGUSR1 to every sidebar pane's pid in the current window
// so visible render loops redraw after a focus/session change. #{P:…} emits only
// sidebar pids; kill's errors are swallowed and the command always succeeds.
func sidebarWakeCmd() string {
	return `run-shell -b "kill -USR1 #{P:#{?#{m:* sidebar render*,#{pane_start_command}},#{pane_pid} ,}} 2>/dev/null; true"`
}

// sidebarHooks returns the full hook table to install at sidebarHookIndex, in a
// deterministic order (ensure, guarded ensure, bounce, wake).
func sidebarHooks(bin string, width int) []sidebarHook {
	hooks := make([]sidebarHook, 0, len(sidebarHookNames()))
	ensure := sidebarEnsureCmd(bin)
	for _, n := range sidebarEnsureHookNames {
		hooks = append(hooks, sidebarHook{Name: n, Cmd: ensure})
	}
	guarded := sidebarGuardedEnsureCmd(bin, width)
	for _, n := range sidebarGuardedEnsureHookNames {
		hooks = append(hooks, sidebarHook{Name: n, Cmd: guarded})
	}
	hooks = append(hooks, sidebarHook{Name: sidebarBounceHookName, Cmd: sidebarBounceCmd()})
	wake := sidebarWakeCmd()
	for _, n := range sidebarWakeHookNames {
		hooks = append(hooks, sidebarHook{Name: n, Cmd: wake})
	}
	return hooks
}

// sidebarHookNames lists every hook name the plugin owns, in install order. It
// is the single source for uninstall so unset mirrors install exactly.
func sidebarHookNames() []string {
	names := make([]string, 0,
		len(sidebarEnsureHookNames)+len(sidebarGuardedEnsureHookNames)+1+len(sidebarWakeHookNames))
	names = append(names, sidebarEnsureHookNames...)
	names = append(names, sidebarGuardedEnsureHookNames...)
	names = append(names, sidebarBounceHookName)
	names = append(names, sidebarWakeHookNames...)
	return names
}

// newSidebarCommand builds the `sidebar` command group. A bare `twm sidebar`
// prints help; the subcommands manage the persistent agents panel.
func newSidebarCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sidebar",
		Short: "Manage the persistent agents sidebar",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return c.Help()
		},
	}
	cmd.AddCommand(newSidebarEnsureCommand())
	cmd.AddCommand(newSidebarRenderCommand())
	cmd.AddCommand(newSidebarInstallCommand())
	cmd.AddCommand(newSidebarUninstallCommand())
	cmd.AddCommand(newSidebarEnableCommand())
	cmd.AddCommand(newSidebarDisableCommand())
	cmd.AddCommand(newSidebarToggleCommand())
	return cmd
}

// newSidebarInstallCommand builds `sidebar install`: register the global hooks
// and dock the sidebar in every window. When the sidebar is resolved-disabled it
// is a pure teardown (uninstall), so the TPM entry can call install
// unconditionally.
func newSidebarInstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "install",
		Short: "Register sidebar tmux hooks and dock the sidebar everywhere",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSidebarInstall()
		},
	}
}

// newSidebarUninstallCommand builds `sidebar uninstall`: unset exactly the hooks
// install registered and kill every sidebar pane.
func newSidebarUninstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "uninstall",
		Short: "Remove sidebar tmux hooks and kill all sidebar panes",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSidebarUninstall()
		},
	}
}

// newSidebarEnableCommand builds `sidebar enable`: turn the sidebar on at runtime
// (@twm_sidebar_enabled=1) and install.
func newSidebarEnableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "enable",
		Short: "Enable the sidebar at runtime and install it",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSidebarSetEnabled(true)
		},
	}
}

// newSidebarDisableCommand builds `sidebar disable`: turn the sidebar off at
// runtime (@twm_sidebar_enabled=0) and tear it down.
func newSidebarDisableCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "disable",
		Short: "Disable the sidebar at runtime and remove it",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSidebarSetEnabled(false)
		},
	}
}

// newSidebarToggleCommand builds `sidebar toggle [-t window]`: flip the per-window
// @twm_sidebar_off option and reconcile that window.
func newSidebarToggleCommand() *cobra.Command {
	var target string
	cmd := &cobra.Command{
		Use:   "toggle",
		Short: "Show or hide the sidebar in one window",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runSidebarToggle(target)
		},
	}
	cmd.Flags().StringVarP(&target, "target", "t", "", "window id to toggle (default: current window)")
	return cmd
}

// runSidebarInstall resolves the enabled state (runtime option over twm.toml). If
// disabled it delegates to uninstall so install is safe to call unconditionally.
// Otherwise it sets every hook at sidebarHookIndex and reconciles all windows in
// process. Hook errors are reported but do not stop the remaining hooks.
func runSidebarInstall() error {
	file, _, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "twm sidebar install:", err)
	}
	runtime := strings.TrimSpace(tmuxcli.DisplayMessage("", "#{"+sidebarEnabledOption+"}"))
	if !config.ResolveEnabled(runtime, file) {
		return runSidebarUninstall()
	}

	bin, err := os.Executable()
	if err != nil {
		return err
	}

	var firstErr error
	for _, h := range sidebarHooks(bin, file.Width) {
		if err := tmuxcli.SetHook(h.Name, sidebarHookIndex, h.Cmd); err != nil {
			fmt.Fprintln(os.Stderr, "twm sidebar install:", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	runSidebarEnsure("")
	return firstErr
}

// runSidebarUninstall unsets exactly the hooks install registered (mirroring
// sidebarHookNames) and kills every sidebar pane. A pane kill never removes a
// window because a sidebar is never the only pane.
func runSidebarUninstall() error {
	var firstErr error
	for _, name := range sidebarHookNames() {
		if err := tmuxcli.UnsetHook(name, sidebarHookIndex); err != nil {
			fmt.Fprintln(os.Stderr, "twm sidebar uninstall:", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}

	panes, err := tmuxcli.ListSidebarGeometry()
	if err != nil {
		fmt.Fprintln(os.Stderr, "twm sidebar uninstall:", err)
		if firstErr == nil {
			firstErr = err
		}
		return firstErr
	}
	for _, p := range panes {
		if !p.Sidebar {
			continue
		}
		if err := tmuxcli.KillPane(p.PaneID); err != nil {
			fmt.Fprintln(os.Stderr, "twm sidebar uninstall:", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// runSidebarSetEnabled sets the runtime @twm_sidebar_enabled option then installs
// (enabled) or uninstalls (disabled). install re-reads the option and resolves
// the same state, so a disabled install collapses to teardown.
func runSidebarSetEnabled(enabled bool) error {
	value := "0"
	if enabled {
		value = "1"
	}
	if err := tmuxcli.SetGlobalOption(sidebarEnabledOption, value); err != nil {
		return err
	}
	if enabled {
		return runSidebarInstall()
	}
	return runSidebarUninstall()
}

// runSidebarToggle flips the per-window @twm_sidebar_off option and reconciles
// that window so the sidebar appears or disappears immediately. An empty target
// resolves to the current window.
func runSidebarToggle(target string) error {
	if target == "" {
		target = strings.TrimSpace(tmuxcli.DisplayMessage("", "#{window_id}"))
	}
	if !tmuxcli.ValidWindowID(target) {
		return tmuxcli.ErrInvalidWindowID
	}
	off := strings.TrimSpace(tmuxcli.DisplayMessage(target, "#{"+sidebarOffOption+"}"))
	next := "1"
	if off == "1" {
		next = "0"
	}
	if err := tmuxcli.SetWindowOption(target, sidebarOffOption, next); err != nil {
		return err
	}
	runSidebarEnsure(target)
	return nil
}

// newSidebarEnsureCommand builds `sidebar ensure [-t window]`, which reconciles
// the sidebar for one window (or all windows) idempotently. It is invoked from
// tmux hooks via run-shell, so it always exits 0 and writes any error to stderr
// where run-shell can surface it for debugging.
func newSidebarEnsureCommand() *cobra.Command {
	var target string
	cmd := &cobra.Command{
		Use:   "ensure",
		Short: "Reconcile sidebar panes for one or all windows",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			runSidebarEnsure(target)
			return nil
		},
	}
	cmd.Flags().StringVarP(&target, "target", "t", "", "window id to reconcile (default: all windows)")
	return cmd
}

// runSidebarEnsure serializes on the ensure lock, re-lists panes under the lock
// so parallel hooks converge, then applies sidebar.Decide per window. Every
// failure is logged to stderr and swallowed; the caller (a tmux hook) never
// sees a non-zero exit.
func runSidebarEnsure(target string) {
	lock, err := sidebar.Lock()
	if err != nil {
		fmt.Fprintln(os.Stderr, "twm sidebar ensure:", err)
		return
	}
	defer lock.Release()

	// Recount after taking the lock so concurrent hooks agree on the pane set.
	// Geometry-only listing: it carries no free-form field, so a hostile pane
	// path can never forge a row that feeds a kill/resize action below.
	panes, err := tmuxcli.ListSidebarGeometry()
	if err != nil {
		fmt.Fprintln(os.Stderr, "twm sidebar ensure:", err)
		return
	}

	// config.Load returns clamped defaults even on ErrConfig, so keep going.
	file, _, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "twm sidebar ensure:", err)
	}

	bin, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "twm sidebar ensure:", err)
		return
	}

	for _, ws := range windowStates(panes, target) {
		enabled := config.ResolveEnabled(ws.runtimeEnabled, file)
		for _, a := range sidebar.Decide(ws.state, file, enabled) {
			if err := execSidebarAction(a, bin); err != nil {
				fmt.Fprintln(os.Stderr, "twm sidebar ensure:", err)
			}
		}
	}
}

// windowEnsure pairs a window's Decide input with the raw runtime enable option
// (@twm_sidebar_enabled) read from its panes.
type windowEnsure struct {
	state          sidebar.WindowState
	runtimeEnabled string
}

// windowStates groups panes into per-window Decide inputs, preserving first-seen
// order for deterministic action ordering. A non-empty target restricts the
// result to that window id.
func windowStates(panes []tmuxcli.SidebarPane, target string) []windowEnsure {
	var order []string
	byWin := make(map[string]*windowEnsure)
	for _, p := range panes {
		if target != "" && p.WindowID != target {
			continue
		}
		we, ok := byWin[p.WindowID]
		if !ok {
			we = &windowEnsure{
				state: sidebar.WindowState{
					WindowID:     p.WindowID,
					WindowWidth:  p.WindowWidth,
					WindowHeight: p.WindowHeight,
					Zoomed:       p.WindowZoomed,
					Off:          p.SidebarOff == "1",
				},
				runtimeEnabled: p.SidebarEnabled,
			}
			byWin[p.WindowID] = we
			order = append(order, p.WindowID)
		}
		if p.Sidebar {
			we.state.Sidebars = append(we.state.Sidebars, sidebar.Pane{
				PaneID: p.PaneID,
				Left:   p.Left,
				Top:    p.Top,
				Width:  p.Width,
				Height: p.Height,
				Active: p.PaneActive,
			})
		} else {
			we.state.RealPanes++
		}
	}
	out := make([]windowEnsure, 0, len(order))
	for _, id := range order {
		out = append(out, *byWin[id])
	}
	return out
}

// execSidebarAction performs one Decide action via the tmux adapters. Create
// splits a new detached sidebar pane running `<bin> sidebar render`.
func execSidebarAction(a sidebar.Action, bin string) error {
	switch a.Type {
	case sidebar.ActionCreate:
		_, err := tmuxcli.SplitSidebar(a.WindowID, bin, a.Width)
		return err
	case sidebar.ActionKill:
		return tmuxcli.KillPane(a.PaneID)
	case sidebar.ActionKillWindow:
		return tmuxcli.KillWindow(a.WindowID)
	case sidebar.ActionResize:
		return tmuxcli.ResizePaneX(a.PaneID, a.Width)
	case sidebar.ActionUnzoom:
		return tmuxcli.UnzoomPane(a.PaneID)
	default:
		return fmt.Errorf("unknown sidebar action %d", a.Type)
	}
}
