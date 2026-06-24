package picker

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FzfExitCancelled is fzf's exit code when the user aborts (Esc / Ctrl-C).
const FzfExitCancelled = 130

// ShellQuote single-quotes s for embedding in a shell command (fzf binds, the
// display-popup command), matching the script's '$self' usage. Embedded single
// quotes are escaped.
func ShellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// WindowFzfOptions builds the fzf arguments for the main window picker. self is
// the path to this binary (os.Executable), embedded into the preview/reload/
// editor bindings so fzf re-invokes the right subcommands.
func WindowFzfOptions(self string) []string {
	q := ShellQuote(self)
	return []string{
		"--ansi", "--reverse", "--no-sort", "--prompt=window > ",
		"--delimiter=\t", "--with-nth=2",
		"--preview=" + q + " preview {1}",
		"--preview-window=right,60%,follow",
		"--bind=ctrl-z:execute-silent(" + q + " open-editor zed {1})+abort," +
			"ctrl-t:execute-silent(" + q + " open-editor typora {1})+abort",
		"--bind=ctrl-r:reload(" + q + " list)",
		"--border",
		"--header=Enter: switch | Ctrl-N: New | Ctrl-Z: Zed | Ctrl-T: Typora",
		"--print-query", "--expect=ctrl-n",
	}
}

// RunFzf runs fzf with the given options, feeding input on stdin and returning
// fzf's stdout, its exit code, and any non-exec error. fzf's stdout/stderr are
// otherwise connected to the terminal (the popup).
func RunFzf(input string, opts []string) (out string, exitCode int, err error) {
	cmd := exec.Command("fzf", opts...)
	cmd.Stdin = strings.NewReader(input)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.Output()
	out = string(stdout)
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return out, ee.ExitCode(), nil // fzf ran; non-zero is a normal signal
		}
		return out, -1, err
	}
	return out, 0, nil
}

// SelectionFiles returns the temp file paths used to hand the popup's selection
// and exit code back to the outer `run` process. client is sanitized so a
// client name containing "/" is safe in a filename (matching the script).
func SelectionFiles(client string) (selFile, errFile string) {
	safe := strings.ReplaceAll(client, "/", "_")
	dir := os.TempDir()
	return filepath.Join(dir, "tmux_wm_sel_"+safe+".txt"),
		filepath.Join(dir, "tmux_wm_err_"+safe+".txt")
}
