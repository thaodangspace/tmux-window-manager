package sidebar

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/thaodangspace/tmux-window-manager/picker"
	"github.com/thaodangspace/tmux-window-manager/store"
)

// Rendering constants: the alternate-screen frame control sequences and the
// bold attribute the picker palette does not provide.
const (
	bold     = "\x1b[1m"
	ellipsis = "…"

	cursorHome = "\x1b[H"
	clearToEOL = "\x1b[K"
	clearToEOS = "\x1b[J"
	lineFeed   = "\r\n"
	emptyState = "no agents running"
	rowIndent  = "  "
	subIndent  = "    "
)

// seg is one styled run of text. An empty style renders the text with no ANSI
// codes; a non-empty style wraps the text in style...reset.
type seg struct {
	text  string
	style string
}

// Render turns a Snapshot into at most height lines, each with a visible width
// (ANSI stripped) of at most width columns. Every externally-sourced string
// (workspace label, agent display name, subtitle) is sanitized before it is
// truncated and coloured. The empty snapshot yields a single dim placeholder.
func Render(s Snapshot, width, height int) []string {
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}

	total := 0
	for _, ws := range s.Workspaces {
		total += len(ws.Agents)
	}
	if total == 0 {
		return []string{styleLine([]seg{{emptyState, picker.Dim}}, width)}
	}

	fullLines := 0
	for _, ws := range s.Workspaces {
		fullLines += 1 + 2*len(ws.Agents)
	}
	overflow := fullLines > height

	keep := make([]bool, total)
	if !overflow {
		for i := range keep {
			keep[i] = true
		}
	} else {
		selectKept(s, keep, height-1)
	}

	kept := 0
	for _, k := range keep {
		if k {
			kept++
		}
	}
	dropped := total - kept

	lines := make([]string, 0, height)
	gi := 0
	for _, ws := range s.Workspaces {
		any := false
		for k := 0; k < len(ws.Agents); k++ {
			if keep[gi+k] {
				any = true
				break
			}
		}
		if any {
			lines = append(lines, styleLine([]seg{{Sanitize(ws.Label), bold}}, width))
			for k, row := range ws.Agents {
				if keep[gi+k] {
					lines = append(lines, rowLine(row, width))
					lines = append(lines, subtitleLine(row, width))
				}
			}
		}
		gi += len(ws.Agents)
	}
	if dropped > 0 {
		lines = append(lines, styleLine([]seg{{fmt.Sprintf("+%d more", dropped), picker.Dim}}, width))
	}
	return lines
}

// selectKept marks which rows survive an overflow, keeping waiting rows first
// then filling the remaining line budget in display order. Each kept row costs
// two lines plus one for its workspace header the first time that workspace is
// used; budget already excludes the reserved "+N more" line.
func selectKept(s Snapshot, keep []bool, budget int) {
	type ref struct {
		global  int
		wsIdx   int
		waiting bool
	}
	refs := make([]ref, 0, len(keep))
	gi := 0
	for wi, ws := range s.Workspaces {
		for _, row := range ws.Agents {
			refs = append(refs, ref{global: gi, wsIdx: wi, waiting: row.Status == store.Waiting})
			gi++
		}
	}

	used := 0
	counted := make(map[int]bool)
	consider := func(waitingOnly bool) {
		for _, r := range refs {
			if keep[r.global] || r.waiting != waitingOnly {
				continue
			}
			need := 2
			if !counted[r.wsIdx] {
				need++
			}
			if used+need <= budget {
				keep[r.global] = true
				used += need
				counted[r.wsIdx] = true
			}
		}
	}
	consider(true)
	consider(false)
}

// rowLine renders one agent as "  <glyph> <Display>  <status>", colouring the
// glyph and status text by state and bolding the display name for the current
// window's agent.
func rowLine(row Row, width int) string {
	glyph, text, color := statusStyle(row)
	displayStyle := ""
	if row.Current {
		displayStyle = bold
	}
	segs := []seg{
		{rowIndent, ""},
		{glyph, color},
		{" ", ""},
		{Sanitize(row.Display), displayStyle},
		{"  ", ""},
		{text, color},
	}
	return styleLine(segs, width)
}

func subtitleLine(row Row, width int) string {
	return styleLine([]seg{{subIndent + Sanitize(row.Subtitle), picker.Dim}}, width)
}

// statusStyle maps a store status (and whether the agent has a live DB row)
// onto its glyph, status text, and colour.
func statusStyle(row Row) (glyph, text, color string) {
	switch row.Status {
	case store.Running:
		return "⋮", "working", picker.Cyan
	case store.Waiting:
		return "●", "waiting", picker.Ylw
	default: // idle
		if row.Hooked {
			return "✓", "idle", picker.Green
		}
		return "○", "idle", picker.Dim
	}
}

// styleLine concatenates styled segments, clamping the visible width to width
// columns and appending an ellipsis when the content is truncated.
func styleLine(segs []seg, width int) string {
	if width <= 0 {
		return ""
	}
	total := 0
	for _, sg := range segs {
		total += displayWidth(sg.text)
	}
	if total <= width {
		return renderClamped(segs, width)
	}
	return renderClamped(segs, width-1) + ellipsis
}

// renderClamped writes segments until limit display columns are consumed,
// wrapping each styled run in style...reset.
func renderClamped(segs []seg, limit int) string {
	var b strings.Builder
	used := 0
	for _, sg := range segs {
		if used >= limit {
			break
		}
		text, w := clampWidth(sg.text, limit-used)
		if text == "" {
			continue
		}
		if sg.style != "" {
			b.WriteString(sg.style)
			b.WriteString(text)
			b.WriteString(picker.Rst)
		} else {
			b.WriteString(text)
		}
		used += w
	}
	return b.String()
}

// truncate clamps a plain string to width display columns, appending an
// ellipsis when it does not fit.
func truncate(s string, width int) string {
	return styleLine([]seg{{text: s}}, width)
}

// clampWidth returns the longest prefix of s whose display width does not
// exceed budget, and that prefix's display width.
func clampWidth(s string, budget int) (string, int) {
	if budget <= 0 {
		return "", 0
	}
	used := 0
	for i, r := range s {
		w := runeWidth(r)
		if used+w > budget {
			return s[:i], used
		}
		used += w
	}
	return s, used
}

// displayWidth is the number of terminal columns s occupies.
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

// runeWidth returns the column count for a single rune: 0 for combining and
// format runes, 2 for wide CJK/emoji, 1 otherwise.
func runeWidth(r rune) int {
	if r == 0 {
		return 0
	}
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
		return 0
	}
	if isWide(r) {
		return 2
	}
	return 1
}

// wideRanges is a small ascending table of code-point ranges rendered as two
// columns (East Asian wide/fullwidth plus common emoji blocks).
var wideRanges = [...]struct{ lo, hi rune }{
	{0x1100, 0x115F},
	{0x2329, 0x232A},
	{0x2E80, 0x303E},
	{0x3041, 0x33FF},
	{0x3400, 0x4DBF},
	{0x4E00, 0x9FFF},
	{0xA000, 0xA4CF},
	{0xAC00, 0xD7A3},
	{0xF900, 0xFAFF},
	{0xFE10, 0xFE19},
	{0xFE30, 0xFE6F},
	{0xFF00, 0xFF60},
	{0xFFE0, 0xFFE6},
	{0x1F300, 0x1F64F},
	{0x1F680, 0x1F6FF},
	{0x1F900, 0x1F9FF},
	{0x1FA70, 0x1FAFF},
	{0x20000, 0x3FFFD},
}

func isWide(r rune) bool {
	for _, rg := range wideRanges {
		if r < rg.lo {
			return false
		}
		if r <= rg.hi {
			return true
		}
	}
	return false
}

// Frame renders lines as an alternate-screen update: home the cursor, emit each
// line cleared to end of line, then clear the rest of the screen.
func Frame(lines []string) []byte {
	var b strings.Builder
	b.WriteString(cursorHome)
	for i, l := range lines {
		if i > 0 {
			b.WriteString(lineFeed)
		}
		b.WriteString(l)
		b.WriteString(clearToEOL)
	}
	b.WriteString(clearToEOS)
	return []byte(b.String())
}
