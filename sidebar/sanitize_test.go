package sidebar

import (
	"strings"
	"testing"
)

func TestSanitizeStripsTerminalControls(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"osc52 clipboard", "a\x1b]52;c;QUJD\x07b", "a]52;c;QUJDb"},
		{"osc0 title", "x\x1b]0;pwned\x07y", "x]0;pwnedy"},
		{"csi color", "red\x1b[31mtext\x1b[0m", "red[31mtext[0m"},
		{"raw c1 csi", "a\u009b31mb", "a31mb"},
		{"nul byte", "a\x00b", "ab"},
		{"tab", "a\tb", "ab"},
		{"newline", "a\nb", "ab"},
		{"del", "a\x7fb", "ab"},
		{"plain ascii", "hello world", "hello world"},
		{"utf8 preserved", "café résumé", "café résumé"},
		{"cjk preserved", "你好世界", "你好世界"},
		{"emoji preserved", "run 🤖 now", "run 🤖 now"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Sanitize(tc.in); got != tc.want {
				t.Fatalf("Sanitize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeLeavesNoControlBytes(t *testing.T) {
	in := "\x1b]52;c;evil\x07\x00\t\x7f\u009b\u0080\u009fclean 你好 🤖"
	got := Sanitize(in)
	for _, r := range got {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			t.Fatalf("Sanitize left control rune %U in %q", r, got)
		}
	}
	if !strings.Contains(got, "clean 你好 🤖") {
		t.Fatalf("Sanitize dropped printable content: %q", got)
	}
}
