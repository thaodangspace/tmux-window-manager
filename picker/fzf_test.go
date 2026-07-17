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
	opts := WindowFzfOptions(self, 160)
	joined := strings.Join(opts, "\x00")

	// The self path must appear shell-quoted in preview/reload/editor binds.
	q := ShellQuote(self)
	for _, want := range []string{
		"--preview=" + q + " preview {1}",
		"--bind=change:reload-sync(" + q + " list --query {q})",
		"--bind=ctrl-r:reload-sync(" + q + " list --query {q})",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("options missing %q\ngot: %v", want, opts)
		}
	}
	if !strings.Contains(joined, "--disabled") {
		t.Errorf("options should disable built-in filtering: %v", opts)
	}
	// Expect-key and print-query must be present for the run-side parser.
	if !strings.Contains(joined, "--expect=ctrl-n") || !strings.Contains(joined, "--print-query") {
		t.Errorf("options missing expect/print-query: %v", opts)
	}
}

func TestWindowFzfOptionsHidesPreviewOnNarrowClients(t *testing.T) {
	tests := []struct {
		name  string
		width int
		want  string
	}{
		{name: "mobile sized", width: MinPreviewClientWidth - 1, want: "--preview-window=hidden"},
		{name: "minimum desktop width", width: MinPreviewClientWidth, want: "--preview-window=right,60%,follow"},
		{name: "unknown width", width: 0, want: "--preview-window=right,60%,follow"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := WindowFzfOptions("/usr/bin/twm", tt.width)
			if !containsOption(opts, tt.want) {
				t.Fatalf("options missing %q: %v", tt.want, opts)
			}
		})
	}
}

func containsOption(opts []string, want string) bool {
	for _, opt := range opts {
		if opt == want {
			return true
		}
	}
	return false
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
