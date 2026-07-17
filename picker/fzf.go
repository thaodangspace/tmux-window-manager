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

// MinPreviewClientWidth is the smallest tmux client width that shows the
// right-hand preview. Narrower clients are treated as mobile-sized so the
// window list can use the full popup width.
const MinPreviewClientWidth = 100

// WindowFzfOptions builds the fzf arguments for the main window picker. self is
// the path to this binary (os.Executable), embedded into the preview/reload/
// editor bindings so fzf re-invokes the right subcommands. A non-positive
// clientWidth means the size could not be detected, so the preview remains
// visible for backwards-compatible behavior.
func WindowFzfOptions(self string, clientWidth int) []string {
	q := ShellQuote(self)
	previewWindow := "right,60%,follow"
	if clientWidth > 0 && clientWidth < MinPreviewClientWidth {
		previewWindow = "hidden"
	}
	return []string{
		"--ansi", "--reverse", "--no-sort", "--prompt=window > ",
		"--disabled",
		"--delimiter=\t", "--with-nth=2",
		"--preview=" + q + " preview {1}",
		"--preview-window=" + previewWindow,
		"--bind=ctrl-z:execute-silent(" + q + " open-editor zed {1})+abort," +
			"ctrl-t:execute-silent(" + q + " open-editor typora {1})+abort",
		"--bind=change:reload-sync(" + q + " list --query {q})",
		"--bind=ctrl-r:reload-sync(" + q + " list --query {q})",
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
