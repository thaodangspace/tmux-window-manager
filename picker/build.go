package picker

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/thaodangspace/tmux-window-manager/store"
	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
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
	AgentLabel string // "claude(opus)" when an agent runs here; hidden search metadata
	PaneLabel  string // "claude"/"pi" as shown by the status-bar label command
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
	return BuildFiltered(e, "")
}

// BuildFiltered returns fzf input rows, optionally narrowed by query while
// preserving matching session headers. A query matching the session keeps the
// whole group; a query matching child rows keeps the header plus matching rows.
func BuildFiltered(e Enricher, query string) (string, error) {
	if e == nil {
		e = NoopEnricher{}
	}
	windows, err := tmuxcli.ListWindows()
	if err != nil {
		return "", err
	}

	var b strings.Builder
	prev := ""
	var group []windowRow
	flush := func(session string) {
		if session == "" {
			return
		}
		rows := filterGroup(session, group, query)
		if len(rows) == 0 {
			return
		}
		writeHeader(&b, session, groupSearch(rows))
		for _, r := range rows {
			writeWindowRow(&b, r)
		}
	}
	for _, w := range windows {
		if w.Session != prev {
			flush(prev)
			group = group[:0]
			prev = w.Session
		}
		group = append(group, newWindowRow(w, e.Window(w.Session, w.Index, w.Name, w.Command)))
	}
	flush(prev)
	return b.String(), nil
}

type windowRow struct {
	target string
	dot    string
	name   string
	robot  string
	status string
	search string
}

func newWindowRow(w tmuxcli.Window, wb WindowBadge) windowRow {
	dot := "  "
	if w.Active {
		dot = Green + "●" + Rst + " "
	}

	robot := ""
	status := ""
	if wb.AgentLabel != "" {
		robot = strings.TrimPrefix(Robot, " ")
		status = strings.TrimPrefix(statusText(wb.Status), " ")
	}

	idx := strconv.Itoa(w.Index)
	target := w.Session + ":" + idx
	return windowRow{
		target: target,
		dot:    dot,
		name:   statusPanelName(w, wb),
		robot:  robot,
		status: status,
		search: cleanSearch(strings.Join([]string{target, w.Session, w.Name, w.Command, w.Path, wb.AgentLabel, wb.PaneLabel, wb.Status}, " ")),
	}
}

func statusPanelName(w tmuxcli.Window, wb WindowBadge) string {
	base := w.Name
	if w.Path != "" {
		base = filepath.Base(w.Path)
	}
	label := w.Command
	if wb.PaneLabel != "" {
		label = wb.PaneLabel
	}
	if label == "" {
		return base
	}
	if base == "" {
		return label
	}
	return base + "/" + label
}

func writeHeader(b *strings.Builder, session, search string) {
	// <session>\t<cyan><session><rst>\t<hidden child search terms>
	fmt.Fprintf(b, "%s\t%s%s%s\t%s\n", session, Cyan, session, Rst, search)
}

func writeWindowRow(b *strings.Builder, r windowRow) {
	// The hidden target stays "session:index" so selecting the row switches to
	// the right window, but the visible row avoids repeating that target or any
	// custom process/agent/model label. Show tmux's window name, optional bot, and
	// optional status in scan-friendly columns.
	parts := make([]string, 0, 3)
	for _, part := range []string{r.name, r.robot, r.status} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	fmt.Fprintf(b, "%s\t   %s%s\t%s\n", r.target, r.dot, strings.Join(parts, " - "), r.search)
}

func groupSearch(rows []windowRow) string {
	terms := make([]string, 0, len(rows))
	for _, r := range rows {
		terms = append(terms, r.search)
	}
	return strings.Join(terms, " ")
}

func cleanSearch(s string) string {
	return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(s)
}

func filterGroup(session string, rows []windowRow, query string) []windowRow {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" || strings.Contains(strings.ToLower(session), q) {
		return rows
	}
	out := make([]windowRow, 0, len(rows))
	for _, r := range rows {
		if strings.Contains(strings.ToLower(r.search), q) {
			out = append(out, r)
		}
	}
	return out
}
