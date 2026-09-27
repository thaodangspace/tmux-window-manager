package picker

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/thaodangspace/tmux-window-manager/dirs"
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

// statusText maps every agent state to a distinct trailing label.
func statusText(status string) string {
	switch status {
	case store.Error:
		return " " + Red + Italic + "error" + Rst
	case store.Waiting:
		return " " + Ylw + Italic + "waiting" + Rst
	case store.Working:
		return " " + Cyan + Italic + "working" + Rst
	case store.Idle:
		return " " + Dim + Italic + "idle" + Rst
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
	return build(e, "", false)
}

// BuildFiltered returns fzf input rows, optionally narrowed by query while
// preserving matching session headers. A query matching the session keeps the
// whole group; a query matching child rows keeps the header plus matching rows.
func BuildFiltered(e Enricher, query string) (string, error) {
	return build(e, query, false)
}

// BuildFilteredAgents is BuildFiltered plus the Ctrl-A agents-only toggle: when
// agentsOnly is true, only windows running a coding agent are kept, along with
// the session headers that still have at least one such window.
func BuildFilteredAgents(e Enricher, query string, agentsOnly bool) (string, error) {
	return build(e, query, agentsOnly)
}

func build(e Enricher, query string, agentsOnly bool) (string, error) {
	if e == nil {
		e = NoopEnricher{}
	}
	windows, err := tmuxcli.ListWindows()
	if err != nil {
		return "", err
	}

	// Pane contents are comparatively expensive to obtain, so only discover
	// panes while the user has an active query. Their bodies are captured lazily
	// below, and only for windows that do not already match metadata.
	panesByWindow := map[string][]tmuxcli.Pane{}
	if strings.TrimSpace(query) != "" {
		for _, pane := range tmuxcli.AllPanes() {
			key := pane.Session + ":" + strconv.Itoa(pane.WindowIndex)
			panesByWindow[key] = append(panesByWindow[key], pane)
		}
	}

	var b strings.Builder
	prev := ""
	var group []windowRow
	flush := func(session string) {
		if session == "" {
			return
		}
		rows := filterGroup(session, group, query, agentsOnly)
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
		r := newWindowRow(w, e.Window(w.Session, w.Index, w.Name, w.Command))
		if !searchMatch(w.Session, query) && !searchMatch(r.search, query) {
			r.content = searchablePaneText(panesByWindow[r.target])
		}
		group = append(group, r)
	}
	flush(prev)
	return b.String(), nil
}

type windowRow struct {
	target   string
	dot      string
	name     string
	robot    string
	status   string
	search   string
	hasAgent bool
	content  string // captured pane body used for matching, never emitted to fzf
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
	branch := ""
	if w.Path != "" {
		branch = dirs.GitBranch(w.Path)
	}
	return windowRow{
		target:   target,
		dot:      dot,
		name:     statusPanelName(w, wb, branch),
		robot:    robot,
		status:   status,
		hasAgent: wb.AgentLabel != "",
		search:   cleanSearch(strings.Join([]string{target, w.Session, w.Name, w.Command, w.Path, branch, wb.AgentLabel, wb.PaneLabel, wb.Status}, " ")),
	}
}

func statusPanelName(w tmuxcli.Window, wb WindowBadge, branch string) string {
	base := w.Name
	if w.Path != "" {
		base = filepath.Base(w.Path)
	}
	if branch != "" {
		base = fmt.Sprintf("%s(%s)", base, branch)
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
	display := r.name
	if r.robot != "" {
		display = r.robot + " " + display
	}
	if r.status != "" {
		display += " - " + r.status
	}
	fmt.Fprintf(b, "%s\t   %s%s\t%s\n", r.target, r.dot, display, r.search)
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

func filterGroup(session string, rows []windowRow, query string, agentsOnly bool) []windowRow {
	sessionMatches := searchMatch(session, query)
	out := make([]windowRow, 0, len(rows))
	for _, r := range rows {
		if agentsOnly && !r.hasAgent {
			continue
		}
		if sessionMatches || searchMatch(r.search, query) || searchMatch(r.content, query) {
			out = append(out, r)
		}
	}
	return out
}

// searchMatch performs case-insensitive substring matching. Query words are
// matched independently, but every word must occur contiguously: "claude"
// never matches "calude" or characters scattered through an opaque ID.
func searchMatch(text, query string) bool {
	haystack := strings.ToLower(text)
	for _, word := range strings.Fields(strings.ToLower(query)) {
		if !strings.Contains(haystack, word) {
			return false
		}
	}
	return true
}

// searchablePaneText captures the same visible pane bodies shown by the
// preview. Capture failures are ignored so a pane disappearing during an fzf
// reload cannot break the picker.
func searchablePaneText(panes []tmuxcli.Pane) string {
	var text strings.Builder
	for _, pane := range panes {
		body, err := tmuxcli.CapturePane(pane.ID, false)
		if err != nil {
			continue
		}
		text.WriteByte(' ')
		text.WriteString(body)
	}
	return text.String()
}
