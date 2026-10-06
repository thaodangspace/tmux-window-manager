package cli

import "github.com/spf13/cobra"

// newHookCommand is a no-op kept so hooks installed by older releases (Claude
// settings.json, Codex notify) keep exiting 0 until `uninstall-hooks` removes
// them. Agent status now comes from the watcher (see watch.go). stdin is not
// read: Codex may leave it attached to something that never closes.
func newHookCommand() *cobra.Command {
	return &cobra.Command{
		Use:                "hook",
		Short:              "Deprecated no-op (agent status comes from the watcher)",
		Hidden:             true,
		DisableFlagParsing: true,
		RunE:               func(*cobra.Command, []string) error { return nil },
	}
}
