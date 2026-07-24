package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/thaodangspace/tmux-window-manager/dirs"
	"github.com/thaodangspace/tmux-window-manager/picker"
	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
	"github.com/spf13/cobra"
)

func newRunCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "run [client]",
		Short: "Open the window picker popup and switch to the selection",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			client := ""
			if len(args) > 0 {
				client = args[0]
			}
			return runOuter(client)
		},
	}
}

// runOuter is the launcher: it opens the picker popup, reads the selection the
// popup wrote, and acts on it (switch window/session, or create a new session).
// The switch must happen AFTER the popup closes — switch-client from inside a
// popup is undone when the popup closes — which is why the popup hands the
// selection back via temp files rather than us reading its stdout.
func runOuter(client string) error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	selFile, errFile := picker.SelectionFiles(client)
	_ = os.Remove(selFile)
	_ = os.Remove(errFile)

	popupCmd := picker.ShellQuote(self) + " popup " + picker.ShellQuote(client)
	// Ignore the popup's exit status (matches the script's `|| true`).
	_ = tmuxcli.Command("display-popup", "-E", "-w", "85%", "-h", "75%", popupCmd)

	errData, err1 := os.ReadFile(errFile)
	selData, err2 := os.ReadFile(selFile)
	_ = os.Remove(selFile)
	_ = os.Remove(errFile)
	if err1 != nil || err2 != nil {
		return nil // popup produced nothing (e.g. tmux unavailable) — no-op
	}
	if strings.TrimSpace(string(errData)) == strconv.Itoa(picker.FzfExitCancelled) {
		return nil // user cancelled
	}

	lines := strings.Split(string(selData), "\n")
	query := lineAt(lines, 0)
	key := lineAt(lines, 1)
	selection := lineAt(lines, 2)

	if key == "ctrl-n" {
		return newSession(client, query)
	}

	target := selection
	if i := strings.IndexByte(target, '\t'); i >= 0 {
		target = target[:i]
	}
	if target == "" {
		return nil
	}
	return switchTo(client, target)
}

// switchTo switches the launching client to the selected target.
func switchTo(client, target string) error {
	return tmuxcli.Command(switchCommand(client, target)...)
}

// switchCommand builds the tmux argv that switches the launching client to the
// target. A window row ("session:index") switches to the session then selects
// the window (one tmux invocation, ";"-separated); a header row ("session")
// switches to the session's current window. When client is non-empty it is
// passed via -c so the right client moves.
func switchCommand(client, target string) []string {
	args := []string{"switch-client"}
	if client != "" {
		args = append(args, "-c", client)
	}
	if session, _, ok := strings.Cut(target, ":"); ok {
		args = append(args, "-t", session, ";", "select-window", "-t", target)
	} else {
		args = append(args, "-t", target)
	}
	return args
}

// newSession runs the Ctrl-N flow: pick/type a directory, then create (or add a
// window to) a session named after the query or the directory basename.
func newSession(client, query string) error {
	currentDir := tmuxcli.DisplayMessage(client, "#{pane_current_path}")
	if currentDir == "" {
		currentDir, _ = os.UserHomeDir()
	}
	promptName := query
	if promptName == "" {
		promptName = "new session"
	}

	dirList := strings.Join(dirs.List(currentDir), "\n")
	out, code, err := runDirFzf(dirList, promptName)
	if err != nil {
		return err
	}
	if code == picker.FzfExitCancelled || strings.TrimSpace(out) == "" {
		return nil
	}

	dl := strings.Split(out, "\n")
	dirQuery := lineAt(dl, 0)
	dirSelection := lineAt(dl, 1)
	targetDir := dirSelection
	if targetDir == "" {
		targetDir = dirQuery
	}
	if targetDir == "" {
		return nil
	}
	targetDir = resolvePath(targetDir, currentDir)
	if info, err := os.Stat(targetDir); err != nil || !info.IsDir() {
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return err
		}
	}

	sessionName := deriveSessionName(query, targetDir)

	switchArgs := []string{"switch-client"}
	if client != "" {
		switchArgs = append(switchArgs, "-c", client)
	}
	switchArgs = append(switchArgs, "-t", sessionName)

	if tmuxcli.HasSession(sessionName) {
		if err := tmuxcli.Command("new-window", "-t", sessionName, "-c", targetDir); err != nil {
			return err
		}
	} else {
		if err := tmuxcli.Command("new-session", "-d", "-s", sessionName, "-c", targetDir); err != nil {
			return err
		}
	}
	return tmuxcli.Command(switchArgs...)
}

// runDirFzf shows the directory picker in fzf's own tmux popup.
func runDirFzf(input, promptName string) (string, int, error) {
	opts := []string{
		"--tmux", "center,85%,75%",
		"--ansi", "--reverse", "--no-sort",
		"--prompt=dir for '" + promptName + "' > ",
		"--print-query",
	}
	cmd := exec.Command("fzf", opts...)
	cmd.Stdin = strings.NewReader(input)
	cmd.Stderr = os.Stderr
	stdout, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return string(stdout), ee.ExitCode(), nil
		}
		return string(stdout), -1, err
	}
	return string(stdout), 0, nil
}

// resolvePath expands a leading ~ and resolves relative inputs against base.
func resolvePath(input, base string) string {
	home, _ := os.UserHomeDir()
	if input == "~" {
		return home
	}
	if strings.HasPrefix(input, "~/") {
		return filepath.Join(home, input[2:])
	}
	if filepath.IsAbs(input) {
		return input
	}
	return filepath.Join(base, input)
}

// deriveSessionName names a new session after the typed query, or the target
// directory's basename when no query was given; ":" is replaced with "-" since
// it is the session:window separator.
func deriveSessionName(query, targetDir string) string {
	name := query
	if name == "" {
		name = filepath.Base(targetDir)
	}
	return strings.ReplaceAll(name, ":", "-")
}

func lineAt(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}
