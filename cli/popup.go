package cli

import (
	"os"
	"strconv"

	"github.com/dtonair/tmux-window-manager/picker"
	"github.com/dtonair/tmux-window-manager/tmuxcli"
	"github.com/spf13/cobra"
)

func newPopupCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "popup [client]",
		Short:  "Run fzf inside the popup and record the selection (internal)",
		Args:   cobra.MaximumNArgs(1),
		Hidden: true,
		RunE: func(_ *cobra.Command, args []string) error {
			client := ""
			if len(args) > 0 {
				client = args[0]
			}
			return runPopup(client)
		},
	}
}

func runPopup(client string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}

	// Build the list straight from the status DB (event-driven; no background
	// scan to spawn). Ctrl-R re-runs `list` to pick up newer hook writes.
	rows, err := picker.Build(picker.NewLiveEnricher(liveStatus()))
	if err != nil {
		return err
	}

	clientWidth := tmuxcli.ClientWidth(client)
	out, code, runErr := picker.RunFzf(rows, picker.WindowFzfOptions(self, clientWidth))
	if runErr != nil {
		return runErr
	}

	// Hand the selection and exit code back to the outer `run` process via temp
	// files — the popup's stdout isn't visible to the launching process.
	selFile, errFile := picker.SelectionFiles(client)
	_ = os.WriteFile(errFile, []byte(strconv.Itoa(code)+"\n"), 0o644)
	_ = os.WriteFile(selFile, []byte(out), 0o644)
	return nil
}
