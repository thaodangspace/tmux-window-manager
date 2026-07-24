// Package cli builds the tmux-window-manager command tree and translates
// command results into process exit codes.
//
// The subcommands mirror the modes of the original tmux_window_manager.sh,
// plus the event-driven agent-status commands:
//
//	run           outer launcher: open popup, read selection, switch-client
//	popup         inside the popup: run fzf, write the selection back
//	list          emit the window list rows (fzf input)
//	preview       render a window's panes (fzf --preview)
//	label         print a pane's agent name for the status bar
//	open-editor   open Zed/Typora on a window's current path
//	hook          record an agent lifecycle event (called from Claude/Codex)
//	install-hooks wire the status hooks into Claude Code and Codex
//	status        dump the live agent status rows (debug)
//
// The binary re-invokes itself for the inner modes; the self-path comes from
// os.Executable() rather than $BASH_SOURCE.
package cli

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// Exit codes: 0 success, 1 runtime error, 2 usage error.
const (
	exitOK      = 0
	exitRuntime = 1
	exitUsage   = 2
)

// usageError marks an error as a usage problem so Execute maps it to exit
// code 2 instead of the default runtime code 1.
type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

// errNotImplemented is the placeholder returned by stub subcommands until their
// implementing phase lands.
var errNotImplemented = errors.New("not implemented")

// version is the CLI version, overridden at build time via
// -ldflags "-X github.com/thaodangspace/tmux-window-manager/cli.version=<value>".
var version = "dev"

func newRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "tmux-window-manager",
		Short:         "Fuzzy tmux window switcher with native agent detection",
		Long:          "tmux-window-manager is a self-contained fuzzy window switcher for tmux.\nIt lists windows across all sessions, previews their panes, and badges windows\nrunning coding agents (claude/codex/pi) — detected natively, no external tools\nbeyond tmux and fzf.",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &usageError{err: err}
	})

	root.AddCommand(
		newRunCommand(),
		newPopupCommand(),
		newListCommand(),
		newPreviewCommand(),
		newLabelCommand(),
		newOpenEditorCommand(),
		newHookCommand(),
		newServeAttachCommand(),
		newInstallHooksCommand(),
		newStatusCommand(),
	)

	return root
}

// Execute runs the root command and returns the process exit code.
func Execute() int {
	ensurePath()
	root := newRootCommand()
	err := root.Execute()
	if err == nil {
		return exitOK
	}
	fmt.Fprintln(os.Stderr, "tmux-window-manager:", err)
	return exitCodeFor(err)
}

// exitCodeFor maps an error to a process exit code: usage errors → 2,
// everything else → 1.
func exitCodeFor(err error) int {
	if err == nil {
		return exitOK
	}
	var ue *usageError
	if errors.As(err, &ue) {
		return exitUsage
	}
	if msg := err.Error(); strings.HasPrefix(msg, "unknown command") ||
		strings.HasPrefix(msg, "unknown flag") ||
		strings.HasPrefix(msg, "unknown shorthand flag") {
		return exitUsage
	}
	return exitRuntime
}
