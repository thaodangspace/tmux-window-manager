package picker

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dtonair/tmux-window-manager/store"
	"github.com/dtonair/tmux-window-manager/tmuxcli"
)

// Each emitted row is "<target>\t<display>":
//
//	target  = "session:index" for windows, or "session" for header rows
//	display = the colored, indented label fzf shows (via --with-nth=2)
//
// Header rows carry the session as target so selecting a session name switches
// to that session's current window. Agent badges live on the individual window
// rows where the agent actually runs, never on the session header.

// WindowBadge describes the agent state of a single window.
type WindowBadge struct {
	AgentLabel string // "claude(opus)" when an agent runs here; empty otherwise
	Status     string // store status for this window's agent
}

// statusText maps an agent status to its trailing italicized label: "waiting"
// when the agent wants the user, "running" when it is working, nothing when
// idle. Text (not a glyph) so claude/codex states read at a glance.
func statusText(status string) string {
	switch status {
	case store.Waiting:
		return " " + Ylw + Italic + "waiting" + Rst
	case store.Running:
		return " " + Cyan + Italic + "running" + Rst
	default:
		return ""
	}
}

// Enricher supplies agent badges for windows. NoopEnricher renders plain
// windows; LiveEnricher derives badges from the status DB.
type Enricher interface {
	Window(session string, index int, name, cmd string) WindowBadge
}

// NoopEnricher renders windows with no agent badges.
type NoopEnricher struct{}

func (NoopEnricher) Window(string, int, string, string) WindowBadge { return WindowBadge{} }

// Build returns the fzf input rows for every window across all sessions.
func Build(e Enricher) (string, error) {
	if e == nil {
		e = NoopEnricher{}
	}
	windows, err := tmuxcli.ListWindows()
	if err != nil {
		return "", err
	}

	var b strings.Builder
	prev := ""
	for _, w := range windows {
		if w.Session != prev {
			writeHeader(&b, w.Session)
			prev = w.Session
		}
		writeWindow(&b, w, e.Window(w.Session, w.Index, w.Name, w.Command))
	}
	return b.String(), nil
}

func writeHeader(b *strings.Builder, session string) {
	// <session>\t<cyan><session><rst>
	fmt.Fprintf(b, "%s\t%s%s%s\n", session, Cyan, session, Rst)
}

func writeWindow(b *strings.Builder, w tmuxcli.Window, wb WindowBadge) {
	dot := "  "
	if w.Active {
		dot = Green + "●" + Rst + " "
	}

	// When an agent runs in this window, its label and status text replace the
	// (usually uninformative) pane command; otherwise show name + command.
	var label string
	if wb.AgentLabel != "" {
		label = Dim + wb.AgentLabel + Rst + Robot + statusText(wb.Status)
	} else {
		label = w.Name + " " + Dim + "(" + w.Command + ")" + Rst
	}

	// The target stays "session:index" so selecting the row switches to the right
	// window, but the dimmed prefix the row displays is the window's own current
	// directory (basename) instead of the session name — so each window is
	// identified by where it actually sits, even within a multi-directory session.
	idx := strconv.Itoa(w.Index)
	dir := filepath.Base(w.Path)
	if w.Path == "" {
		dir = w.Session
	}
	fmt.Fprintf(b, "%s:%s\t   %s%s%s:%s%s %s\n",
		w.Session, idx, dot, Dim, dir, idx, Rst, label)
}
