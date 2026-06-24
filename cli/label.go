package cli

import (
	"fmt"
	"strings"

	"github.com/dtonair/tmux-window-manager/agents"
	"github.com/spf13/cobra"
)

func newLabelCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "label <pid> [fallback]",
		Short:  "Print the agent name for a pane pid (status bar)",
		Args:   cobra.RangeArgs(1, 2),
		Hidden: true,
		// SilenceErrors/Usage at the root keep this quiet; we also never return
		// an error here so the status bar never shows diagnostics.
		RunE: func(cmd *cobra.Command, args []string) error {
			fallback := ""
			if len(args) > 1 {
				fallback = args[1]
			}
			// Process-tree detection only (no transcript cache) — matches the
			// script's --label, which runs per window every status interval.
			names := agents.NewDetector().Names(args[0])
			out := fallback
			if len(names) > 0 {
				out = strings.Join(names, " ")
			}
			fmt.Fprint(cmd.OutOrStdout(), out) // no trailing newline, like printf '%s'
			return nil
		},
	}
}
