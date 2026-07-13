package cli

import (
	"fmt"

	"github.com/dtonair/tmux-window-manager/picker"
	"github.com/dtonair/tmux-window-manager/store"
	"github.com/spf13/cobra"
)

func newListCommand() *cobra.Command {
	var query string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Emit the window list rows consumed by fzf",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Agent presence, name, model, and status all come from the status
			// DB, which agents push to via lifecycle hooks. A missing/unreadable
			// DB degrades to a plain window list (no badges).
			rows, err := picker.BuildFiltered(picker.NewLiveEnricher(liveStatus()), query)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	cmd.Flags().StringVar(&query, "query", "", "filter rows while preserving matching session headers")
	return cmd
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
