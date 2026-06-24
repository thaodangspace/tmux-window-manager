// Package preview renders the fzf preview pane: every pane of the target
// window, stacked and labeled — a faithful port of the script's --preview mode.
package preview

import (
	"fmt"
	"io"
	"strings"

	"github.com/dtonair/tmux-window-manager/agents"
	"github.com/dtonair/tmux-window-manager/picker"
	"github.com/dtonair/tmux-window-manager/tmuxcli"
)

// Render writes the preview for target (a "session:index" spec) to w.
func Render(w io.Writer, target string) {
	if target == "" {
		return
	}
	det := agents.NewDetector()
	panes := tmuxcli.PanesOf(target)
	renderPanes(w, panes, det)
}

func renderPanes(w io.Writer, panes []tmuxcli.PaneDetail, det *agents.Detector) {
	for i, p := range panes {
		if i > 0 {
			fmt.Fprintln(w)
		}
		mark := ""
		if p.Active {
			mark = " " + picker.Green + "●" + picker.Rst
		}
		// Agent name beats the (often-bogus) pane_current_command version string.
		pname := p.Command
		if names := det.Names(p.PID); len(names) > 0 {
			pname = strings.Join(names, " ")
		}
		fmt.Fprintf(w, "%s── pane %d%s  %s  [%dx%d] %s\n",
			picker.Dim, p.Index, mark, pname, p.Width, p.Height, picker.Rst)

		text, _ := tmuxcli.CapturePane(p.ID, true)
		fmt.Fprint(w, trimTrailingBlankLines(text))
	}
}

// trimTrailingBlankLines drops trailing all-whitespace lines so panes stack
// compactly — the port of the script's awk last-non-blank trim. The returned
// text ends with a newline when non-empty.
func trimTrailingBlankLines(s string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	last := -1
	for i, ln := range lines {
		if strings.TrimSpace(ln) != "" {
			last = i
		}
	}
	if last < 0 {
		return ""
	}
	return strings.Join(lines[:last+1], "\n") + "\n"
}
