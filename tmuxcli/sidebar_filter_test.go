package tmuxcli

import (
	"path"
	"strings"
	"testing"
)

// sidebarGlob is the tmux `#{m:...}` glob embedded in NotSidebarFilter. Phase 10
// isolation relies on it classifying every sidebar pane_start_command as a
// sidebar while leaving real panes untouched.
const sidebarGlob = "* sidebar render*"

// TestNotSidebarFilterContainsGlob pins NotSidebarFilter to the contains-form
// glob confirmed on tmux 3.5a (docs/plans/sidebar-spike/FINDINGS.md). A suffix
// marker fails there because tmux re-quotes multi-word start commands.
func TestNotSidebarFilterContainsGlob(t *testing.T) {
	want := "#{m:" + sidebarGlob + ",#{pane_start_command}}"
	if !strings.Contains(NotSidebarFilter, want) {
		t.Fatalf("NotSidebarFilter = %q, missing glob %q", NotSidebarFilter, want)
	}
}

// TestSidebarGlobMatch evaluates sidebarGlob with path.Match against
// representative pane_start_command forms. path.Match faithfully models tmux's
// `#{m:...}` fnmatch for these bins; note its `*` does not cross '/', so the
// bins here are slash-free (a realistic slash path is covered by
// TestSidebarGlobSlashPath).
func TestSidebarGlobMatch(t *testing.T) {
	tests := []struct {
		name    string
		cmd     string
		sidebar bool
	}{
		{"plain form", "'twm' sidebar render", true},
		{"tmux re-quoted form", `"'twm' sidebar render"`, true},
		{"vim editing sidebar source", "vim sidebar.go", false},
		{"plain shell", "zsh", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match, err := path.Match(sidebarGlob, tt.cmd)
			if err != nil {
				t.Fatalf("path.Match(%q, %q) error: %v", sidebarGlob, tt.cmd, err)
			}
			if match != tt.sidebar {
				t.Fatalf("path.Match(%q, %q) = %v, want %v", sidebarGlob, tt.cmd, match, tt.sidebar)
			}
		})
	}
}

// TestSidebarGlobSlashPath documents that path.Match only approximates tmux's
// `#{m:...}`: its `*` stops at '/', so a realistic bin path re-quoted by tmux is
// a false negative under path.Match even though tmux (fnmatch, no FNM_PATHNAME)
// and IsSidebarStartCommand both match it. Phase 10 relies on the tmux-side
// semantics, so NotSidebarFilter still excludes such panes in practice.
func TestSidebarGlobSlashPath(t *testing.T) {
	const cmd = `"'/opt/homebrew/bin/twm' sidebar render"`
	if match, _ := path.Match(sidebarGlob, cmd); match {
		t.Fatalf("path.Match unexpectedly crossed '/': %q", cmd)
	}
	if !IsSidebarStartCommand(cmd) {
		t.Fatalf("IsSidebarStartCommand(%q) = false, want true", cmd)
	}
}
