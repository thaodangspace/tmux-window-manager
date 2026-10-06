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
// the path to this binary (os.Executable), embedded into the preview/reload
// bindings so fzf re-invokes the right subcommands. client identifies the
// launching tmux client so the Ctrl-A agents-only toggle and reloads share a
// per-client state file. A non-positive clientWidth means the size could not be
// detected, so the preview remains visible for backwards-compatible behavior.
func WindowFzfOptions(self, client string, clientWidth int) []string {
	q := ShellQuote(self)
	c := ShellQuote(client)
	previewWindow := "right,60%,follow"
	if clientWidth > 0 && clientWidth < MinPreviewClientWidth {
		previewWindow = "hidden"
	}
	// reload rebuilds rows from the status DB, honoring the current query and
	// the per-client agents-only toggle; each reload is a fresh process, so it
	// reads that toggle from a temp file instead of inheriting state.
	reload := "reload-sync(" + q + " list --query {q} --client " + c + ")"
	return []string{
		"--ansi", "--reverse", "--no-sort", "--prompt=window > ",
		"--disabled",
		"--delimiter=\t", "--with-nth=2",
		"--preview=" + q + " preview {1}",
		"--preview-window=" + previewWindow,
		"--bind=change:" + reload,
		"--bind=ctrl-r:" + reload,
		"--bind=ctrl-a:execute-silent(" + q + " toggle-agents " + c + ")+" + reload,
		"--bind=ctrl-x:execute-silent(" + q + " kill-session " + c + " {1})+" + reload,
		"--border",
		"--header=Enter: switch | Ctrl-N: New | Ctrl-X: Kill window/session | Ctrl-A: Agents only",
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

// AgentsFilterFile returns the per-client temp file whose presence means the
// picker is showing only windows that run a coding agent. Ctrl-A toggles it
// through the `toggle-agents` subcommand; because every fzf reload spawns a
// fresh `list` process that cannot inherit in-memory state, `list` reads this
// file back on each reload. client is sanitized the same way as SelectionFiles.
func AgentsFilterFile(client string) string {
	safe := strings.ReplaceAll(client, "/", "_")
	return filepath.Join(os.TempDir(), "tmux_wm_agents_"+safe+".txt")
}

// AgentsOnly reports whether the per-client agents-only filter is currently on.
func AgentsOnly(client string) bool {
	_, err := os.Stat(AgentsFilterFile(client))
	return err == nil
}

// ToggleAgentsOnly flips the per-client agents-only filter and returns the new
// state. A missing/unwritable file is treated as "off" so the toggle can never
// wedge the picker.
func ToggleAgentsOnly(client string) bool {
	path := AgentsFilterFile(client)
	if _, err := os.Stat(path); err == nil {
		_ = os.Remove(path)
		return false
	}
	_ = os.WriteFile(path, []byte("1"), 0o644)
	return true
}

// ClearAgentsOnly removes the per-client agents-only filter so each popup opens
// showing every window rather than inheriting a previous run's toggle.
func ClearAgentsOnly(client string) {
	_ = os.Remove(AgentsFilterFile(client))
}
