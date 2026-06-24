package picker

import (
	"strings"
	"testing"
)

func TestShellQuote(t *testing.T) {
	tests := []struct{ in, want string }{
		{"/usr/bin/twm", "'/usr/bin/twm'"},
		{"/path with spaces/twm", "'/path with spaces/twm'"},
		{"/it's/here", `'/it'\''s/here'`},
	}
	for _, tt := range tests {
		if got := ShellQuote(tt.in); got != tt.want {
			t.Errorf("ShellQuote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestWindowFzfOptionsEmbedsSelf(t *testing.T) {
	self := "/path with spaces/tmux-window-manager"
	opts := WindowFzfOptions(self)
	joined := strings.Join(opts, "\x00")

	// The self path must appear shell-quoted in preview/reload/editor binds.
	q := ShellQuote(self)
	for _, want := range []string{
		"--preview=" + q + " preview {1}",
		"--bind=ctrl-r:reload(" + q + " list)",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("options missing %q\ngot: %v", want, opts)
		}
	}
	// Expect-key and print-query must be present for the run-side parser.
	if !strings.Contains(joined, "--expect=ctrl-n") || !strings.Contains(joined, "--print-query") {
		t.Errorf("options missing expect/print-query: %v", opts)
	}
}

func TestSelectionFilesSanitizesClient(t *testing.T) {
	sel, errf := SelectionFiles("/dev/ttys003")
	if strings.Contains(sel[strings.LastIndex(sel, "tmux_wm_sel_"):], "/dev/") {
		t.Errorf("client slashes not sanitized in %q", sel)
	}
	if !strings.Contains(sel, "tmux_wm_sel_") || !strings.Contains(errf, "tmux_wm_err_") {
		t.Errorf("unexpected temp file names: %q %q", sel, errf)
	}
}
