package cli

import (
	"github.com/thaodangspace/tmux-window-manager/preview"
	"github.com/spf13/cobra"
)

func newPreviewCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "preview <target>",
		Short:  "Render a window's panes (fzf preview)",
		Args:   cobra.ExactArgs(1),
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			preview.Render(cmd.OutOrStdout(), args[0])
			return nil
		},
	}
}
