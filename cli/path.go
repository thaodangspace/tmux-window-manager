package cli

import (
	"os"
	"strings"
)

// commonBinDirs are prepended to PATH so the tools we shell out to (fzf, tmux,
// zed/typora, open) are found even when launched from tmux's run-shell, which
// provides a minimal environment. This mirrors the original script's explicit
// `export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"`.
var commonBinDirs = []string{"/opt/homebrew/bin", "/usr/local/bin"}

// ensurePath prepends commonBinDirs to PATH when missing, preserving existing
// entries and their order.
func ensurePath() {
	path := os.Getenv("PATH")
	have := make(map[string]bool)
	for _, p := range strings.Split(path, string(os.PathListSeparator)) {
		have[p] = true
	}
	var prefix []string
	for _, d := range commonBinDirs {
		if !have[d] {
			prefix = append(prefix, d)
		}
	}
	if len(prefix) == 0 {
		return
	}
	if path != "" {
		prefix = append(prefix, path)
	}
	_ = os.Setenv("PATH", strings.Join(prefix, string(os.PathListSeparator)))
}
