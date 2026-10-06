package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/tmux-window-manager/notify"
	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
)

// macosSender posts a Notification Center alert through alerter. alerter only
// reports a click while it is still running, so the watcher hands it to a
// detached `notify-wait` process and returns immediately.
type macosSender struct {
	cfg notify.MacOSConfig
	// spawn starts argv detached; a test seam.
	spawn func(argv []string) error
}

func newMacOSSender(cfg notify.MacOSConfig) *macosSender {
	return &macosSender{cfg: cfg, spawn: spawnDetached}
}

func (m *macosSender) Notify(_ context.Context, event notify.Event) error {
	alerter, err := notify.AlerterArgv(m.cfg, event)
	if err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return notify.ErrNotifierFailed
	}
	group := notify.MacOSGroup(event)
	// The replaced notification's waiter would otherwise linger until timeout.
	stopWaiter(group)
	argv := []string{self, "notify-wait", "--group", group}
	if tmuxcli.ValidPaneID(event.Pane) {
		argv = append(argv, "--pane", event.Pane)
		if event.TmuxSocket != "" {
			argv = append(argv, "--socket", event.TmuxSocket)
		}
	}
	if m.cfg.TerminalBundleID != "" {
		argv = append(argv, "--activate", m.cfg.TerminalBundleID)
	}
	argv = append(argv, "--")
	argv = append(argv, alerter...)
	if m.spawn(argv) != nil {
		return notify.ErrNotifierFailed
	}
	return nil
}

// spawnDetached starts argv in its own session with stdio on /dev/null, so it
// outlives the watcher scan and never holds its pipes open.
func spawnDetached(argv []string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// waiterPIDFile records the notify-wait process of one notification group.
func waiterPIDFile(group string) string {
	return filepath.Join(os.TempDir(), "twm_notify_"+group+".pid")
}

// stopWaiter terminates the previous waiter of group (and its alerter child,
// which shares its process group). The pid is trusted only while it still
// runs notify-wait, so a recycled pid is never signalled.
func stopWaiter(group string) {
	path := waiterPIDFile(group)
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	_ = os.Remove(path)
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		return
	}
	out, err := exec.Command("ps", "-o", "command=", "-p", strconv.Itoa(pid)).Output()
	if err != nil || !strings.Contains(string(out), " notify-wait ") {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
}

// newNotifyWaitCommand runs alerter, waits for the user to act on the
// notification, and on a click focuses the agent pane and raises the terminal.
func newNotifyWaitCommand() *cobra.Command {
	var group, pane, socket, activate string
	cmd := &cobra.Command{
		Use:    "notify-wait --group G [--pane P] [--socket S] [--activate BUNDLE] -- alerter-argv...",
		Short:  "Wait for a macOS notification click, then jump to the pane",
		Args:   cobra.MinimumNArgs(1),
		Hidden: true,
		RunE: func(_ *cobra.Command, args []string) error {
			notifyWait(group, pane, socket, activate, args)
			return nil
		},
	}
	cmd.Flags().StringVar(&group, "group", "", "notification group")
	cmd.Flags().StringVar(&pane, "pane", "", "tmux pane to focus on click")
	cmd.Flags().StringVar(&socket, "socket", "", "tmux server socket path")
	cmd.Flags().StringVar(&activate, "activate", "", "bundle id of the app to raise on click")
	return cmd
}

// Test seams for the click side effects.
var (
	waitRun = func(ctx context.Context, argv []string) ([]byte, error) {
		return exec.CommandContext(ctx, argv[0], argv[1:]...).Output()
	}
	waitJump     = jump
	waitActivate = func(bundle string) error { return exec.Command("open", "-b", bundle).Run() }
)

func notifyWait(group, pane, socket, activate string, alerter []string) {
	pidFile := ""
	if group != "" {
		pidFile = waiterPIDFile(group)
		self := strconv.Itoa(os.Getpid())
		_ = os.WriteFile(pidFile, []byte(self), 0o600)
		defer func() {
			if data, err := os.ReadFile(pidFile); err == nil && strings.TrimSpace(string(data)) == self {
				_ = os.Remove(pidFile)
			}
		}()
	}

	out, err := waitRun(context.Background(), alerter)
	if err != nil {
		debugf("notify-wait: alerter: %v", err)
	}
	clicked := notify.Clicked(out)
	debugf("notify-wait: %s clicked=%v", group, clicked)
	if !clicked {
		return
	}
	if pane != "" {
		if socket != "" {
			tmuxcli.Socket = socket
		}
		if err := waitJump(pane); err != nil {
			debugf("notify-wait: jump %s: %v", pane, err)
		}
	}
	if activate != "" {
		if err := waitActivate(activate); err != nil {
			debugf("notify-wait: activate %s: %v", activate, err)
		}
	}
}
