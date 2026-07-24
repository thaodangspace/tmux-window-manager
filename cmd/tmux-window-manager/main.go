// Command tmux-window-manager is a self-contained Go port of the
// tmux_window_manager.sh dotfiles script: a fuzzy window switcher for tmux
// (prefix + w) with native agent detection, distributed as a TPM plugin.
package main

import (
	"os"

	"github.com/thaodangspace/tmux-window-manager/cli"
)

func main() {
	os.Exit(cli.Execute())
}
