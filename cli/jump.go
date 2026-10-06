package cli

import (
	"errors"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
)

// newJumpCommand focuses an agent pane in the most recently active tmux
// client. macOS notification clicks do the same in-process (notify-wait);
// --socket names the server when run outside tmux.
func newJumpCommand() *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:    "jump <pane-id>",
		Short:  "Switch the most recent tmux client to a pane",
		Args:   cobra.ExactArgs(1),
		Hidden: true,
		RunE: func(_ *cobra.Command, args []string) error {
			if socket != "" {
				tmuxcli.Socket = socket
			}
			return jump(args[0])
		},
	}
	cmd.Flags().StringVar(&socket, "socket", "", "tmux server socket path")
	return cmd
}

var errNoClient = errors.New("no attached tmux client")

func jump(pane string) error {
	if !tmuxcli.ValidPaneID(pane) {
		return &usageError{err: tmuxcli.ErrInvalidPaneID}
	}
	if ok, err := tmuxcli.PaneExists(pane); err != nil || !ok {
		return errors.New("pane " + pane + " no longer exists")
	}
	clients, err := tmuxcli.ListClients()
	if err != nil {
		return err
	}
	client, ok := tmuxcli.MostRecentClient(clients)
	if !ok {
		return errNoClient
	}
	return tmuxcli.SwitchClient(client.Name, pane)
}
