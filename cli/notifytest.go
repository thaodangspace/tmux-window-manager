package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/tmux-window-manager/notify"
	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
)

// newNotifyTestCommand sends a sample event through every enabled backend,
// bypassing their filters, so a broken setup shows up here instead of only in
// the debug log.
func newNotifyTestCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "notify-test",
		Short: "Send a test notification through every enabled backend",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			pane := os.Getenv("TMUX_PANE")
			if !tmuxcli.ValidPaneID(pane) {
				pane = ""
			}
			event := notify.Event{
				Kind:       notify.Waiting,
				Agent:      "twm",
				SessionID:  "notify-test",
				Detail:     "Test notification from tmux-window-manager",
				Pane:       pane,
				TmuxSocket: tmuxcli.SocketFromEnv(os.Getenv("TMUX")),
			}
			if wd, err := os.Getwd(); err == nil {
				event.Cwd = wd
			}
			return notifyTest(cmd.OutOrStdout(), notifiersFromConfig(), event)
		},
	}
}

var errNotifyTestFailed = errors.New("some backends failed")

func notifyTest(out io.Writer, backends []backend, event notify.Event) error {
	if len(backends) == 0 {
		fmt.Fprintln(out, "no notification backend is enabled in twm.toml ([telegram] or [macos] enabled = true)")
		return nil
	}
	failed := false
	for _, b := range backends {
		err := b.err
		if err == nil && b.sender != nil {
			err = b.sender.Notify(context.Background(), event)
		}
		if err == nil {
			fmt.Fprintf(out, "%s: sent\n", b.name)
			continue
		}
		failed = true
		fmt.Fprintf(out, "%s: failed (%s)%s\n", b.name, notifyErrorCategory(err), notifyTestHint(err))
	}
	if failed {
		return errNotifyTestFailed
	}
	return nil
}

func notifyTestHint(err error) string {
	switch {
	case errors.Is(err, notify.ErrNotifierMissing):
		return " — install it with `brew install alerter`"
	case errors.Is(err, notify.ErrPartialConfig):
		return " — set both bot_token and chat_id"
	case errors.Is(err, notify.ErrConfigFile):
		return " — check that twm.toml is valid TOML"
	}
	return ""
}
