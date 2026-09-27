package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/thaodangspace/tmux-window-manager/picker"
	"github.com/thaodangspace/tmux-window-manager/store"
)

func newListCommand() *cobra.Command {
	var query string
	var client string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Emit the window list rows consumed by fzf",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Agent presence, name, model, and status all come from the status
			// DB, which agents push to via lifecycle hooks. A missing/unreadable
			// DB degrades to a plain window list (no badges).
			//
			// client locates the per-client agents-only toggle file written by
			// Ctrl-A (`toggle-agents`), which narrows the list to agent windows.
			rows, err := picker.BuildFilteredAgents(picker.NewLiveEnricher(liveStatus()), query, picker.AgentsOnly(client))
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&query, "query", "", "filter rows while preserving matching session headers")
	cmd.Flags().StringVar(&client, "client", "", "tmux client whose agents-only toggle applies")
	return cmd
}

// newToggleAgentsCommand flips the picker's per-client agents-only filter. It is
// bound to Ctrl-A in fzf; fzf then reloads `list`, which reads the same file.
func newToggleAgentsCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "toggle-agents [client]",
		Short:  "Toggle the picker's agents-only filter (internal)",
		Args:   cobra.MaximumNArgs(1),
		Hidden: true,
		RunE: func(_ *cobra.Command, args []string) error {
			client := ""
			if len(args) > 0 {
				client = args[0]
			}
			picker.ToggleAgentsOnly(client)
			return nil
		},
	}
}

// liveStatus reads the live agent status rows, returning nil if the DB is
// missing or unreadable so the picker still works without agent metadata.
func liveStatus() []store.Status {
	db, err := store.Open()
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.Live()
	if err != nil {
		return nil
	}
	return rows
}
