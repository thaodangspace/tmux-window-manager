package sidebar

import (
	"regexp"
	"strings"
	"testing"

	"github.com/thaodangspace/tmux-window-manager/store"
)

// sgrRe matches the only escape sequences Render is allowed to emit: SGR
// (colour/attribute) codes of the form ESC [ <params> m. Stripping these should
// leave text with zero control bytes.
var sgrRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// assertNoControlRunes fails if s (after our own SGR codes are removed) still
// contains any C0 control (0x00-0x1F, which includes a bare ESC), DEL, or C1
// control (U+0080-U+009F). This is the terminal-injection invariant: nothing an
// external string smuggles in may survive as an active control byte.
func assertNoControlRunes(t *testing.T, context, line string) {
	t.Helper()
	stripped := sgrRe.ReplaceAllString(line, "")
	for _, r := range stripped {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Fatalf("%s: line %q left control rune %U after SGR strip", context, line, r)
		}
	}
}

// TestRenderNeverEmitsForeignControls feeds a battery of terminal-injection
// payloads through every external string slot (workspace label, agent display
// name, subtitle) and asserts no output line carries a control byte other than
// the ESC of our own SGR codes.
func TestRenderNeverEmitsForeignControls(t *testing.T) {
	payloads := []struct {
		name string
		s    string
	}{
		{"osc52 clipboard", "\x1b]52;c;QUJDRUY=\x07"},
		{"osc0 title", "\x1b]0;pwned\x07"},
		{"osc0 st terminated", "\x1b]0;pwned\x1b\\"},
		{"csi cursor move", "\x1b[10;10Hmoved\x1b[2J"},
		{"csi erase", "\x1b[1;1H\x1b[K"},
		{"8bit c1 csi", "\u009b31mred"},
		{"8bit c1 osc", "\u009d52;c;QUJD\u009c"},
		{"carriage return", "line1\rline2"},
		{"nul and bel", "a\x00b\x07c"},
		{"tab and vtab", "a\tb\x0bc"},
		{"bare esc at end", "trailing-esc\x1b"},
		{"bare esc at start", "\x1bleading"},
		{"mixed c1 range", "x\u0080\u0085\u009fy"},
		{"del byte", "a\x7fb"},
	}

	statuses := []string{store.Running, store.Waiting, store.Idle}

	for _, p := range payloads {
		p := p
		t.Run(p.name, func(t *testing.T) {
			for _, st := range statuses {
				// Inject the payload into every external slot at once.
				s := snap(p.s+"-workspace",
					row("agent"+p.s, st, true, true, "subtitle"+p.s),
					row(p.s, st, false, false, p.s),
				)
				for _, w := range []int{20, 40, 80} {
					for _, l := range Render(s, w, 200) {
						assertNoControlRunes(t, p.name, l)
						if got := displayWidth(sgrRe.ReplaceAllString(l, "")); got > w {
							t.Fatalf("%s: width %d exceeded, got %d in %q", p.name, w, got, l)
						}
					}
				}
			}
		})
	}
}

// TestRenderCJKNeverExceedsNarrowWidth guards the wide-rune truncation path: a
// label and rows composed entirely of two-column CJK runes must still fit a
// 20-column pane on every emitted line.
func TestRenderCJKNeverExceedsNarrowWidth(t *testing.T) {
	cjk := strings.Repeat("測試視窗管理器", 6) // 42 wide runes = 84 columns
	s := snap(cjk,
		row(cjk, store.Running, true, true, cjk),
		row(cjk, store.Waiting, true, false, cjk),
	)
	for _, l := range Render(s, 20, 200) {
		if got := displayWidth(sgrRe.ReplaceAllString(l, "")); got > 20 {
			t.Fatalf("CJK line %q visible width %d exceeds 20", sgrRe.ReplaceAllString(l, ""), got)
		}
	}
}

// TestRenderHeightNeverExceeded confirms the line budget holds for tiny panes:
// no matter how many agents exist, Render never returns more lines than height
// for heights 1, 2 and 3.
func TestRenderHeightNeverExceeded(t *testing.T) {
	s := snap("proj",
		row("Runner1", store.Running, true, false, "s:1"),
		row("Waiter1", store.Waiting, true, false, "s:2"),
		row("Runner2", store.Running, true, true, "s:3"),
		row("Waiter2", store.Waiting, false, false, "s:4"),
		row("Idle1", store.Idle, true, false, "s:5"),
	)
	for _, h := range []int{1, 2, 3} {
		lines := Render(s, 40, h)
		if len(lines) > h {
			t.Fatalf("height %d: got %d lines: %v", h, len(lines), lines)
		}
	}
}

// TestRenderNonPositiveDimensions ensures degenerate pane geometry never panics
// and always yields at least one clamped line.
func TestRenderNonPositiveDimensions(t *testing.T) {
	cases := []struct{ w, h int }{
		{0, 0}, {-1, -1}, {-5, 10}, {10, -5}, {0, 5}, {5, 0},
	}
	populated := snap("proj",
		row("Agent", store.Running, true, true, "sub"),
		row("Other", store.Waiting, true, false, "sub2"),
	)
	for _, c := range cases {
		for _, s := range []Snapshot{{}, populated} {
			lines := Render(s, c.w, c.h) // must not panic
			for _, l := range lines {
				w := c.w
				if w < 1 {
					w = 1
				}
				if got := displayWidth(sgrRe.ReplaceAllString(l, "")); got > w {
					t.Fatalf("w=%d h=%d: line %q width %d exceeds clamp %d", c.w, c.h, l, got, w)
				}
			}
		}
	}
}
