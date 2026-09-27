package cli

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/tmux-window-manager/config"
	"github.com/thaodangspace/tmux-window-manager/dirs"
	"github.com/thaodangspace/tmux-window-manager/sidebar"
	"github.com/thaodangspace/tmux-window-manager/store"
	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
	"golang.org/x/sys/unix"
)

// newSidebarRenderCommand builds the hidden `sidebar render` subcommand: the
// long-lived process tmux runs inside each sidebar pane. It draws the agents
// panel, redrawing only on change while visible and sleeping while hidden, and
// exits cleanly on SIGHUP/SIGTERM or when the sidebar is disabled. It is hidden
// because users never invoke it directly — it is the split-window start command.
func newSidebarRenderCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "render",
		Short:  "Run the sidebar render loop (internal; started by tmux)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			runSidebarRender()
			return nil
		},
	}
}

// runSidebarRender wires the production dependencies into sidebar.Loop. A DB that
// fails to open is not fatal: the loop renders with no live rows (agents still
// appear from the process table, just without status/model). Signals are split
// into a wake set (re-render) and a terminate set (clean exit); SIGINT/SIGQUIT/
// SIGTSTP are ignored so the pane cannot be interrupted or suspended.
func runSidebarRender() {
	self := os.Getenv("TMUX_PANE")
	home, _ := os.UserHomeDir()

	// store.Open failure → render with Live=nil (step 8). A nil *store.DB would
	// panic through the interface, so leave the interface itself nil.
	var db sidebar.DB
	if opened, err := store.Open(); err == nil {
		db = opened
		defer opened.Close()
	} else {
		fmt.Fprintln(os.Stderr, "twm sidebar render:", err)
	}

	wake := make(chan os.Signal, 1)
	signal.Notify(wake, syscall.SIGUSR1, syscall.SIGWINCH)
	defer signal.Stop(wake)

	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGHUP, syscall.SIGTERM)
	defer signal.Stop(term)

	// Do not let a stray interrupt/suspend disturb the pane.
	signal.Ignore(syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTSTP)

	initial := config.DefaultSidebar()
	if cfg, _, err := config.Load(); err == nil {
		initial = cfg
	}

	deps := sidebar.Deps{
		Self:       self,
		Home:       home,
		GitRoot:    dirs.GitRoot,
		ListPanes:  tmuxcli.ListSidebarPanes,
		Snapshot:   psSnapshot,
		DB:         db,
		Config:     loadSidebarConfig,
		Size:       terminalSize,
		ResizeSelf: func(width int) { _ = tmuxcli.ResizePaneX(self, width) },
		Out:        os.Stdout,
		Ticker:     sidebar.NewTicker(time.Duration(initial.RefreshMS) * time.Millisecond),
		Wake:       wake,
		Term:       term,
		Refresh:    time.Duration(initial.RefreshMS) * time.Millisecond,
	}

	if err := sidebar.Loop(deps); err != nil {
		fmt.Fprintln(os.Stderr, "twm sidebar render:", err)
	}
}

// psSnapshot captures the process table for the Detector (same shape the agents
// package parses).
func psSnapshot() string {
	out, _ := exec.Command("ps", "-axo", "pid=,ppid=,comm=").Output()
	return string(out)
}

// loadSidebarConfig loads the sidebar config and reports the file's mtime in unix
// nanoseconds (0 when the file is missing or cannot be stat'd), so the loop can
// skip re-reading an unchanged file.
func loadSidebarConfig() (config.Sidebar, int64, error) {
	cfg, path, err := config.Load()
	var mtime int64
	if path != "" {
		if fi, statErr := os.Stat(path); statErr == nil {
			mtime = fi.ModTime().UnixNano()
		}
	}
	return cfg, mtime, err
}

// terminalSize reports the sidebar pane's (width, height) via its controlling
// pty; a failure falls back to a conservative 24x80-derived default so a render
// still happens.
func terminalSize() (int, int) {
	ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws == nil || ws.Col == 0 || ws.Row == 0 {
		return 32, 24
	}
	return int(ws.Col), int(ws.Row)
}
