package cli

import (
	"fmt"
	"sort"
	"text/tabwriter"

	"github.com/thaodangspace/tmux-window-manager/store"
	"github.com/spf13/cobra"
)

// newStatusCommand dumps the live agent status rows as a table — a debugging
// aid to inspect what the hooks have recorded and what the picker will show.
func newStatusCommand() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show recorded agent status rows (debug)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			db, err := store.Open()
			if err != nil {
				return err
			}
			defer db.Close()

			var rows []store.Status
			if all {
				rows, err = db.All()
			} else {
				m, e := db.LiveByCwd()
				err = e
				for _, s := range m {
					rows = append(rows, s)
				}
			}
			if err != nil {
				return err
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].Cwd < rows[j].Cwd })

			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "AGENT\tSTATUS\tPID\tMODEL\tCWD\tDETAIL")
			for _, s := range rows {
				fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\n",
					s.Agent, s.Status, s.Pid, s.Model, s.Cwd, s.Detail)
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "include rows whose process is no longer alive")
	return cmd
}
