// Package tmuxcli provides thin, typed wrappers over the tmux command-line.
// Every call shells out to the tmux binary with an explicit -F format string
// and parses the tab-delimited result, so the rest of the program never deals
// with raw tmux output.
package tmuxcli

import (
	"bufio"
	"bytes"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// sep is the field separator we ask tmux to emit. Tab is safe because tmux
// formats never contain literal tabs in the fields we request.
const sep = "\t"

// run executes `tmux args...` and returns stdout. tmux errors (e.g. no server)
// are returned to the caller, which generally treats them as "no data".
func run(args ...string) (string, error) {
	out, err := exec.Command("tmux", args...).Output()
	return string(out), err
}

// Window is one tmux window across all sessions.
type Window struct {
	Session string
	Index   int
	Active  bool
	Name    string
	Command string // pane_current_command of the window's active pane
	Path    string // pane_current_path of the window's active pane
}

// ListWindows returns every window across all sessions, sorted by session name
// then window index — matching the bash `tmux list-windows -a | sort` pipeline.
func ListWindows() ([]Window, error) {
	const format = "#{session_name}" + sep + "#{window_index}" + sep +
		"#{window_active}" + sep + "#{window_name}" + sep +
		"#{pane_current_command}" + sep + "#{pane_current_path}"
	out, err := run("list-windows", "-a", "-F", format)
	if err != nil {
		return nil, err
	}

	var ws []Window
	sc := bufio.NewScanner(bytes.NewBufferString(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		f := strings.SplitN(line, sep, 6)
		if len(f) < 6 {
			continue
		}
		idx, _ := strconv.Atoi(f[1])
		ws = append(ws, Window{
			Session: f[0],
			Index:   idx,
			Active:  f[2] == "1",
			Name:    f[3],
			Command: f[4],
			Path:    f[5],
		})
	}
	sort.SliceStable(ws, func(i, j int) bool {
		if ws[i].Session != ws[j].Session {
			return ws[i].Session < ws[j].Session
		}
		return ws[i].Index < ws[j].Index
	})
	return ws, sc.Err()
}

// Pane is one tmux pane, used for the single-call snapshot that drives list
// enrichment (so we spawn tmux once, not per session/window).
type Pane struct {
	Session     string
	WindowIndex int
	ID          string // pane_id, e.g. "%3"
	PID         string // pane_pid
	Path        string // pane_current_path
	Active      bool
}

// AllPanes returns every pane across all sessions in a single tmux call.
func AllPanes() []Pane {
	const format = "#{session_name}" + sep + "#{window_index}" + sep +
		"#{pane_id}" + sep + "#{pane_pid}" + sep + "#{pane_active}" + sep +
		"#{pane_current_path}"
	out, err := run("list-panes", "-a", "-F", format)
	if err != nil {
		return nil
	}
	var panes []Pane
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, sep, 6)
		if len(f) < 6 {
			continue
		}
		idx, _ := strconv.Atoi(f[1])
		panes = append(panes, Pane{
			Session:     f[0],
			WindowIndex: idx,
			ID:          f[2],
			PID:         f[3],
			Active:      f[4] == "1",
			Path:        f[5],
		})
	}
	return panes
}

// PanePIDs returns the pane_pid of every pane matching the given list-panes
// args (e.g. "-s", "-t", session, or "-t", "session:index"). Used by agent
// detection to find the process roots of a window or session.
func PanePIDs(args ...string) []string {
	a := append([]string{"list-panes"}, args...)
	a = append(a, "-F", "#{pane_pid}")
	out, err := run(a...)
	if err != nil {
		return nil
	}
	return nonEmptyLines(out)
}

// PaneDetail is the per-pane info the preview renders.
type PaneDetail struct {
	ID      string
	Index   int
	Command string
	Width   int
	Height  int
	Active  bool
	PID     string
}

// PanesOf returns the panes of a single target (session:window) with the detail
// fields the preview needs.
func PanesOf(target string) []PaneDetail {
	const format = "#{pane_id}" + sep + "#{pane_index}" + sep +
		"#{pane_current_command}" + sep + "#{pane_width}" + sep +
		"#{pane_height}" + sep + "#{pane_active}" + sep + "#{pane_pid}"
	out, err := run("list-panes", "-t", target, "-F", format)
	if err != nil {
		return nil
	}
	var panes []PaneDetail
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, sep, 7)
		if len(f) < 7 {
			continue
		}
		idx, _ := strconv.Atoi(f[1])
		w, _ := strconv.Atoi(f[3])
		h, _ := strconv.Atoi(f[4])
		panes = append(panes, PaneDetail{
			ID: f[0], Index: idx, Command: f[2],
			Width: w, Height: h, Active: f[5] == "1", PID: f[6],
		})
	}
	return panes
}

// PaneIDs returns the pane_id of every pane matching the given list-panes args.
func PaneIDs(args ...string) []string {
	a := append([]string{"list-panes"}, args...)
	a = append(a, "-F", "#{pane_id}")
	out, err := run(a...)
	if err != nil {
		return nil
	}
	return nonEmptyLines(out)
}

// CapturePane returns the visible text of a pane (-p), optionally with escape
// sequences (-e). target is a pane id or session:window.target spec.
func CapturePane(target string, withEscapes bool) (string, error) {
	args := []string{"capture-pane", "-p"}
	if withEscapes {
		args = append(args, "-e")
	}
	args = append(args, "-t", target)
	return run(args...)
}

// ClientWidth returns the width in columns of targetClient. A zero result means
// tmux could not resolve the client or did not return a valid width.
func ClientWidth(targetClient string) int {
	args := []string{"display-message", "-p"}
	if targetClient != "" {
		args = append(args, "-c", targetClient)
	}
	args = append(args, "#{client_width}")
	out, err := run(args...)
	if err != nil {
		return 0
	}
	width, _ := strconv.Atoi(strings.TrimSpace(out))
	return width
}

// DisplayMessage prints a tmux format string evaluated against target (or the
// active pane when target is empty) and returns the result.
func DisplayMessage(target, format string) string {
	args := []string{"display-message", "-p"}
	if target != "" {
		args = append(args, "-t", target)
	}
	args = append(args, format)
	out, _ := run(args...)
	return strings.TrimRight(out, "\n")
}

// HasSession reports whether a session with the given name exists.
func HasSession(name string) bool {
	_, err := run("has-session", "-t", name)
	return err == nil
}

// Command runs an arbitrary tmux command, inheriting no stdio, and returns any
// error. Used for state-changing commands (switch-client, new-session, the
// compound switch+select) and for display-popup.
func Command(args ...string) error {
	return exec.Command("tmux", args...).Run()
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}
