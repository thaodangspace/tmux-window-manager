package sidebar

import "strings"

// Sanitize strips every control byte that a terminal could interpret as an
// escape sequence, so untrusted text (agent prompts, cwd paths, tmux names)
// cannot smuggle a terminal-escape injection into the rendered frame.
//
// It removes:
//   - C0 controls U+0000..U+001F — this includes ESC (U+001B, the lead byte of
//     every OSC/CSI sequence, e.g. OSC 52 clipboard writes and OSC 0/2 title
//     rewrites), NUL, BEL, tab, and newline;
//   - DEL (U+007F);
//   - C1 controls U+0080..U+009F — this includes the raw single-byte CSI
//     (U+009B) and the 8-bit forms of OSC/DCS.
//
// Everything else, including plain UTF-8 and wide CJK/emoji runes, is preserved
// verbatim. Callers still truncate the result to the pane width.
func Sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
