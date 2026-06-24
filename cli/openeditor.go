package cli

import (
	"os"
	"os/exec"

	"github.com/dtonair/tmux-window-manager/tmuxcli"
	"github.com/spf13/cobra"
)

func newOpenEditorCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "open-editor <zed|typora> <target>",
		Short:  "Open an editor on a window's current path (fzf binding)",
		Args:   cobra.ExactArgs(2),
		Hidden: true,
		RunE: func(_ *cobra.Command, args []string) error {
			openEditor(args[0], args[1])
			return nil // never fail the picker on an editor problem
		},
	}
}

// openEditor opens the given editor on the target window's current directory,
// preferring the CLI binary and falling back to `open -a` — the port of the
// script's --open-editor mode.
func openEditor(editor, target string) {
	dir := tmuxcli.DisplayMessage(target, "#{pane_current_path}")
	if dir == "" {
		return
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return
	}

	var cliName, appName string
	switch editor {
	case "zed":
		cliName, appName = "zed", "Zed"
	case "typora":
		cliName, appName = "typora", "Typora"
	default:
		return
	}

	if path, err := exec.LookPath(cliName); err == nil {
		_ = exec.Command(path, dir).Start()
		return
	}
	_ = exec.Command("open", "-a", appName, dir).Start()
}
