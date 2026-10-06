package cli

import "github.com/spf13/cobra"

// newKillSessionCommand lets fzf remove a selected session without exiting.
func newKillSessionCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "kill-session [client] [target]",
		Short:  "Kill the selected window or session from the picker (internal)",
		Hidden: true,
		Args:   cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			if args[1] == "" {
				return nil
			}
			return killSelected(args[0], args[1])
		},
	}
}
