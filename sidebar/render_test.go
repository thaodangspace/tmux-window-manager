package sidebar

import (
	"regexp"
	"strings"
	"testing"

	"github.com/thaodangspace/tmux-window-manager/picker"
	"github.com/thaodangspace/tmux-window-manager/store"
)

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

func row(display, status string, hooked, current bool, subtitle string) Row {
	return Row{Display: display, Status: status, Hooked: hooked, Current: current, Subtitle: subtitle}
}

func snap(label string, rows ...Row) Snapshot {
	return Snapshot{Workspaces: []Workspace{{Key: label, Label: label, Agents: rows}}}
}

func TestRenderVisibleWidthWithinBounds(t *testing.T) {
	s := snap("a-very-long-workspace-name-that-overflows",
		row("Claude Code with an extremely long display name", store.Running, true, true, "editing a file with a really long path/name.go"),
		row("Codex", store.Waiting, true, false, "please approve this diff which is quite long indeed"),
	)
	for _, w := range []int{20, 32, 80} {
		lines := Render(s, w, 100)
		for _, l := range lines {
			if got := displayWidth(stripANSI(l)); got > w {
				t.Fatalf("width %d: line %q visible width %d exceeds %d", w, stripANSI(l), got, w)
			}
		}
	}
}

func TestRenderTruncatesWideRunes(t *testing.T) {
	s := snap("proj",
		row("你好世界你好世界你好", store.Running, true, false, "🤖🤖🤖🤖🤖🤖🤖🤖🤖🤖"),
	)
	lines := Render(s, 12, 100)
	for _, l := range lines {
		if got := displayWidth(stripANSI(l)); got > 12 {
			t.Fatalf("line %q visible width %d exceeds 12", stripANSI(l), got)
		}
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, ellipsis) {
		t.Fatalf("expected an ellipsis in truncated output, got %q", joined)
	}
}

func TestRenderGlyphColorMapping(t *testing.T) {
	tests := []struct {
		name        string
		status      string
		hooked      bool
		glyph, text string
		color       string
	}{
		{"working", store.Running, true, "⋮", "working", picker.Cyan},
		{"waiting", store.Waiting, true, "●", "waiting", picker.Ylw},
		{"idle hooked", store.Idle, true, "✓", "idle", picker.Green},
		{"idle hookless", store.Idle, false, "○", "idle", picker.Dim},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lines := Render(snap("proj", row("Agent", tc.status, tc.hooked, false, "sub")), 80, 100)
			// lines[0] header, lines[1] row, lines[2] subtitle.
			r := lines[1]
			plain := stripANSI(r)
			if !strings.Contains(plain, tc.glyph) {
				t.Fatalf("row %q missing glyph %q", plain, tc.glyph)
			}
			if !strings.Contains(plain, tc.text) {
				t.Fatalf("row %q missing status text %q", plain, tc.text)
			}
			if !strings.Contains(r, tc.color) {
				t.Fatalf("row %q missing colour %q", r, tc.color)
			}
		})
	}
}

func TestRenderBoldOnCurrent(t *testing.T) {
	cur := Render(snap("proj", row("Agent", store.Running, true, true, "sub")), 80, 100)[1]
	if !strings.Contains(cur, bold) {
		t.Fatalf("current row %q missing bold code", cur)
	}
	notCur := Render(snap("proj", row("Agent", store.Running, true, false, "sub")), 80, 100)[1]
	// The header is bold, so check the row line only: strip and confirm no
	// bold precedes the display name for a non-current row.
	if strings.Contains(notCur, bold+"Agent") {
		t.Fatalf("non-current row %q should not bold the display name", notCur)
	}
}

func TestRenderHeaderIsBold(t *testing.T) {
	header := Render(snap("proj", row("Agent", store.Running, true, false, "sub")), 80, 100)[0]
	if !strings.Contains(header, bold) || !strings.Contains(stripANSI(header), "proj") {
		t.Fatalf("header %q should be bold and contain the label", header)
	}
}

func TestRenderOverflowKeepsWaitingRows(t *testing.T) {
	s := snap("proj",
		row("Runner1", store.Running, true, false, "s:1"),
		row("Waiter", store.Waiting, true, false, "s:2"),
		row("Runner2", store.Running, true, false, "s:3"),
	)
	// Full render is 1 header + 3*2 = 7 lines; height 4 forces overflow.
	lines := Render(s, 80, 4)
	if len(lines) > 4 {
		t.Fatalf("expected at most 4 lines, got %d: %v", len(lines), lines)
	}
	joined := stripANSI(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "Waiter") {
		t.Fatalf("waiting row dropped in overflow: %q", joined)
	}
	if strings.Contains(joined, "Runner1") || strings.Contains(joined, "Runner2") {
		t.Fatalf("running rows should be dropped before the waiting row: %q", joined)
	}
	if !strings.Contains(joined, "+2 more") {
		t.Fatalf("expected \"+2 more\" overflow line: %q", joined)
	}
}

func TestRenderEmptyState(t *testing.T) {
	lines := Render(Snapshot{}, 80, 100)
	if len(lines) != 1 {
		t.Fatalf("empty snapshot should render one line, got %d", len(lines))
	}
	if got := stripANSI(lines[0]); got != "no agents running" {
		t.Fatalf("empty state = %q, want %q", got, "no agents running")
	}
	if !strings.Contains(lines[0], picker.Dim) {
		t.Fatalf("empty state should be dim: %q", lines[0])
	}
}

func TestRenderSanitizesExternalStrings(t *testing.T) {
	s := snap("pr\x1b]0;evil\x07oj",
		row("Cla\x1bude", store.Running, true, false, "sub\x9btitle"),
	)
	joined := strings.Join(Render(s, 80, 100), "\n")
	// The OSC introducer (ESC ]) and the raw C1 CSI must be gone; the picker
	// palette still legitimately uses the CSI form ESC [ for colours.
	if strings.Contains(joined, "\x1b]") {
		t.Fatalf("OSC introducer survived sanitization: %q", joined)
	}
	if strings.ContainsRune(joined, 0x9b) {
		t.Fatalf("raw C1 CSI survived sanitization: %q", joined)
	}
	// The inert leftover text is fine and should remain visible.
	if !strings.Contains(stripANSI(joined), "Claude") {
		t.Fatalf("display name lost its printable content: %q", joined)
	}
}

func TestFrameExactBytes(t *testing.T) {
	got := string(Frame([]string{"a", "b"}))
	want := "\x1b[Ha\x1b[K\r\nb\x1b[K\x1b[J"
	if got != want {
		t.Fatalf("Frame = %q, want %q", got, want)
	}
}

func TestFrameEmpty(t *testing.T) {
	got := string(Frame(nil))
	want := "\x1b[H\x1b[J"
	if got != want {
		t.Fatalf("Frame(nil) = %q, want %q", got, want)
	}
}

func TestTruncatePlainString(t *testing.T) {
	if got := truncate("abcdef", 4); got != "abc"+ellipsis {
		t.Fatalf("truncate = %q, want %q", got, "abc"+ellipsis)
	}
	if got := truncate("abc", 8); got != "abc" {
		t.Fatalf("truncate no-op = %q, want %q", got, "abc")
	}
}
