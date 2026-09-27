package tmuxcli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Errors for the sidebar id/name validators. Ids passed to tmux are restricted
// to canonical, tmux-generated forms so a hostile pane path or option value can
// never turn a wrapper call into arbitrary target selection.
var (
	// ErrInvalidWindowID means a caller supplied something other than a
	// canonical tmux window id such as @3.
	ErrInvalidWindowID = errors.New("invalid tmux window id")
	// ErrInvalidHookName means a hook name contained characters outside the
	// tmux hook-name alphabet.
	ErrInvalidHookName = errors.New("invalid tmux hook name")
	// ErrInvalidOption means an option name was empty or contained characters
	// outside the tmux option-name alphabet.
	ErrInvalidOption = errors.New("invalid tmux option name")
)

// sidebarMarker is the substring that identifies a sidebar pane by its
// pane_start_command. tmux re-quotes multi-word start commands (the stored form
// of `'<bin>' sidebar render` is `"'<bin>' sidebar render"`), so the marker is a
// CONTAINS check on " sidebar render", never a suffix. See docs/plans/
// sidebar-spike/FINDINGS.md.
const sidebarMarker = " sidebar render"

// NotSidebarFilter is a tmux `-f` filter expression that keeps real (non-sidebar)
// panes. It mirrors sidebarMarker via the tmux glob `#{m:* sidebar render*,…}`.
const NotSidebarFilter = "#{?#{m:* sidebar render*,#{pane_start_command}},0,1}"

// QuoteBin single-quotes bin so a path with spaces or shell metacharacters is
// passed to the shell verbatim; an embedded single quote is escaped in the
// POSIX '\” form. It is the single source of binary-path quoting shared by the
// sidebar start command and the hook commands.
func QuoteBin(bin string) string {
	return "'" + strings.ReplaceAll(bin, "'", `'\''`) + "'"
}

// SidebarStartCommand returns the command tmux runs for a sidebar pane. bin is
// single-quoted (see QuoteBin) so a path with spaces or shell metacharacters is
// passed verbatim.
func SidebarStartCommand(bin string) string {
	return QuoteBin(bin) + sidebarMarker
}

// IsSidebarStartCommand reports whether a pane_start_command belongs to a
// sidebar pane. It is a CONTAINS check because tmux re-quotes the stored start
// command (see sidebarMarker).
func IsSidebarStartCommand(s string) bool {
	return strings.Contains(s, sidebarMarker)
}

// ValidWindowID reports whether target is a canonical tmux window id such as @3.
func ValidWindowID(target string) bool {
	if len(target) < 2 || target[0] != '@' {
		return false
	}
	for _, r := range target[1:] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// validHookName reports whether name is a plausible tmux hook name (lowercase
// letters and hyphens only, e.g. after-new-window).
func validHookName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r < 'a' || r > 'z') && r != '-' {
			return false
		}
	}
	return true
}

// validOptionName reports whether name is a plausible tmux option name: a
// leading letter or user-option '@', then letters, digits, '_' or '-'.
func validOptionName(name string) bool {
	if name == "" {
		return false
	}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case r == '@' && i == 0:
		case i > 0 && (r >= '0' && r <= '9' || r == '_' || r == '-'):
		default:
			return false
		}
	}
	return true
}

// SidebarPane is one tmux pane with the geometry and lifecycle fields the
// sidebar ensure/render logic needs. ListSidebarGeometry fills the ids, geometry,
// and flags for the destructive paths; ListSidebarPanes additionally fills
// Session and Path for read-only rendering. Sidebar reports whether this row is
// itself a sidebar pane. SidebarOff/SidebarEnabled hold constrained tmux-computed
// tokens ("1"/"0", plus "" for an unset enabled override), not raw option values.
type SidebarPane struct {
	Session         string
	WindowIndex     int
	WindowID        string // window_id, e.g. "@3"
	PaneID          string // pane_id, e.g. "%7"
	PanePID         string // pane_pid
	Path            string // pane_current_path (render listing only)
	StartCommand    string // pane_start_command (test fixtures only; listings compute Sidebar tmux-side)
	Left            int    // pane_left
	Top             int    // pane_top
	Width           int    // pane_width
	Height          int    // pane_height
	WindowWidth     int    // window_width
	WindowHeight    int    // window_height
	WindowActive    bool   // window_active
	SessionAttached bool   // session_attached > 0
	WindowZoomed    bool   // window_zoomed_flag
	PaneActive      bool   // pane_active
	SidebarOff      string // @twm_sidebar_off token ("1"/"0")
	SidebarEnabled  string // @twm_sidebar_enabled token ("1"/"0"/"")
	Sidebar         bool   // whether this pane runs `<bin> sidebar render`
}

// sidebarMarkerField is the tmux-side sidebar test: it emits "1" for a sidebar
// pane and "0" otherwise, computed from pane_start_command via the same glob as
// sidebarMarker. Emitting a constrained flag (never the raw, free-form start
// command) means no listing field can carry an attacker-chosen tab/newline that
// would forge or shift a row.
const sidebarMarkerField = "#{?#{m:* sidebar render*,#{pane_start_command}},1,0}"

// sidebarOffField emits "1" when @twm_sidebar_off is exactly "1", else "0", so a
// hostile option value cannot smuggle a separator into the listing.
const sidebarOffField = "#{?#{==:#{@twm_sidebar_off},1},1,0}"

// sidebarEnabledField emits the tri-state @twm_sidebar_enabled runtime override
// as one of the constrained tokens "1", "0" or "" (empty = defer to the file
// config; see config.ResolveEnabled), never the raw option value.
const sidebarEnabledField = "#{?#{==:#{@twm_sidebar_enabled},1},1,#{?#{==:#{@twm_sidebar_enabled},0},0,}}"

// sidebarGeometryFormat is the -F format for ListSidebarGeometry. Every field is
// a tmux id, a number, or a constrained flag/token — there is NO free-form field
// (no pane_current_path, no pane_start_command, no session_name, no raw option
// value), so no field can contain a tab or newline. Its parser can therefore
// demand an exact field count and reject anything that does not parse, which
// closes the row-forgery vector on the destructive ensure/uninstall paths. Field
// order must stay in lockstep with parseSidebarGeometry.
const sidebarGeometryFormat = "#{window_id}" + sep + "#{pane_id}" + sep +
	"#{pane_left}" + sep + "#{pane_top}" + sep + "#{pane_width}" + sep +
	"#{pane_height}" + sep + "#{window_width}" + sep + "#{window_height}" + sep +
	"#{window_zoomed_flag}" + sep + "#{pane_active}" + sep +
	sidebarMarkerField + sep + sidebarOffField + sep + sidebarEnabledField

const sidebarGeometryFields = 13

// ListSidebarGeometry returns every pane with only the ids, geometry, and
// lifecycle flags the destructive ensure/uninstall paths need. It carries no
// free-form field, so a directory name or option value can never forge a row
// that feeds KillPane/KillWindow/ResizePaneX/UnzoomPane. Read-only rendering uses
// ListSidebarPanes (which also carries pane_current_path) instead.
func ListSidebarGeometry() ([]SidebarPane, error) {
	out, err := run("list-panes", "-a", "-F", sidebarGeometryFormat)
	if err != nil {
		return nil, err
	}
	return parseSidebarGeometry(out), nil
}

// parseSidebarGeometry parses ListSidebarGeometry output. It rejects any line
// whose field count is not exactly sidebarGeometryFields, whose window/pane id is
// not canonical, or whose numeric fields do not parse. Because no field is
// free-form, a well-formed line cannot have been forged from a hostile path.
func parseSidebarGeometry(out string) []SidebarPane {
	var panes []SidebarPane
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		f := strings.Split(line, sep)
		if len(f) != sidebarGeometryFields {
			continue
		}
		if !ValidWindowID(f[0]) || !ValidPaneID(f[1]) {
			continue
		}
		left, err1 := strconv.Atoi(f[2])
		top, err2 := strconv.Atoi(f[3])
		w, err3 := strconv.Atoi(f[4])
		h, err4 := strconv.Atoi(f[5])
		ww, err5 := strconv.Atoi(f[6])
		wh, err6 := strconv.Atoi(f[7])
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil || err5 != nil || err6 != nil {
			continue
		}
		panes = append(panes, SidebarPane{
			WindowID:       f[0],
			PaneID:         f[1],
			Left:           left,
			Top:            top,
			Width:          w,
			Height:         h,
			WindowWidth:    ww,
			WindowHeight:   wh,
			WindowZoomed:   f[8] == "1",
			PaneActive:     f[9] == "1",
			Sidebar:        f[10] == "1",
			SidebarOff:     f[11],
			SidebarEnabled: f[12],
		})
	}
	return panes
}

// sidebarPaneFormat is the -F format for ListSidebarPanes (the read-only render
// path). The single free-form field, pane_current_path, is placed LAST so a tab
// inside it is absorbed by the final field and cannot shift any earlier column;
// pane_start_command is replaced by the tmux-computed sidebarMarkerField, and the
// options are emitted as constrained tokens. Field order must stay in lockstep
// with parseSidebarPanes.
const sidebarPaneFormat = "#{window_index}" + sep + "#{window_id}" + sep +
	"#{pane_id}" + sep + "#{pane_pid}" + sep + "#{window_active}" + sep +
	"#{session_attached}" + sep + "#{window_zoomed_flag}" + sep +
	"#{pane_active}" + sep + sidebarMarkerField + sep + sidebarOffField + sep +
	sidebarEnabledField + sep + "#{session_name}" + sep + "#{pane_current_path}"

const sidebarPaneFields = 13

// ListSidebarPanes returns every pane across all sessions with the fields the
// read-only render/Collect path needs, including pane_current_path, in a single
// tmux call. Destructive paths must use ListSidebarGeometry instead.
func ListSidebarPanes() ([]SidebarPane, error) {
	out, err := run("list-panes", "-a", "-F", sidebarPaneFormat)
	if err != nil {
		return nil, err
	}
	return parseSidebarPanes(out), nil
}

// parseSidebarPanes parses ListSidebarPanes output. It rejects any line that does
// not split into exactly sidebarPaneFields fields, whose window/pane id is not
// canonical, or whose numeric fields do not parse. pane_current_path is the final
// field, so SplitN folds any embedded tabs back into it without corrupting the
// validated columns.
func parseSidebarPanes(out string) []SidebarPane {
	var panes []SidebarPane
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, sep, sidebarPaneFields)
		if len(f) != sidebarPaneFields {
			continue
		}
		wi, err1 := strconv.Atoi(f[0])
		if err1 != nil {
			continue
		}
		if !ValidWindowID(f[1]) || !ValidPaneID(f[2]) {
			continue
		}
		if _, err := strconv.Atoi(f[3]); err != nil { // pane_pid
			continue
		}
		attached, err2 := strconv.Atoi(f[5])
		if err2 != nil {
			continue
		}
		panes = append(panes, SidebarPane{
			WindowIndex:     wi,
			WindowID:        f[1],
			PaneID:          f[2],
			PanePID:         f[3],
			WindowActive:    f[4] == "1",
			SessionAttached: attached > 0,
			WindowZoomed:    f[6] == "1",
			PaneActive:      f[7] == "1",
			Sidebar:         f[8] == "1",
			SidebarOff:      f[9],
			SidebarEnabled:  f[10],
			Session:         f[11],
			Path:            f[12],
		})
	}
	return panes
}

// SplitSidebarArgs builds the split-window argv that docks a sidebar pane on the
// left of windowID at the given width. The pane is created detached (-d) and
// tmux prints the new pane id (-P -F).
func SplitSidebarArgs(windowID, bin string, width int) ([]string, error) {
	if !ValidWindowID(windowID) {
		return nil, ErrInvalidWindowID
	}
	if width <= 0 {
		return nil, fmt.Errorf("invalid sidebar width %d", width)
	}
	return []string{
		"split-window", "-hbf", "-d",
		"-t", windowID,
		"-l", strconv.Itoa(width),
		"-P", "-F", "#{pane_id}",
		SidebarStartCommand(bin),
	}, nil
}

// SplitSidebar docks a sidebar pane on the left of windowID and disables its
// input (select-pane -d) so the user cannot type into it. It returns the new
// pane id.
func SplitSidebar(windowID, bin string, width int) (string, error) {
	args, err := SplitSidebarArgs(windowID, bin, width)
	if err != nil {
		return "", err
	}
	out, err := run(args...)
	if err != nil {
		return "", err
	}
	paneID := strings.TrimSpace(out)
	if !ValidPaneID(paneID) {
		return "", ErrInvalidPaneID
	}
	if err := Command("select-pane", "-d", "-t", paneID); err != nil {
		return paneID, err
	}
	return paneID, nil
}

// KillPaneArgs builds the kill-pane argv for a canonical pane id.
func KillPaneArgs(paneID string) ([]string, error) {
	if !ValidPaneID(paneID) {
		return nil, ErrInvalidPaneID
	}
	return []string{"kill-pane", "-t", paneID}, nil
}

// KillPane kills the given pane.
func KillPane(paneID string) error {
	args, err := KillPaneArgs(paneID)
	if err != nil {
		return err
	}
	return Command(args...)
}

// KillWindowArgs builds the kill-window argv for a canonical window id.
func KillWindowArgs(windowID string) ([]string, error) {
	if !ValidWindowID(windowID) {
		return nil, ErrInvalidWindowID
	}
	return []string{"kill-window", "-t", windowID}, nil
}

// KillWindow kills the given window.
func KillWindow(windowID string) error {
	args, err := KillWindowArgs(windowID)
	if err != nil {
		return err
	}
	return Command(args...)
}

// ResizePaneXArgs builds the resize-pane argv that sets a pane's width.
func ResizePaneXArgs(paneID string, width int) ([]string, error) {
	if !ValidPaneID(paneID) {
		return nil, ErrInvalidPaneID
	}
	if width <= 0 {
		return nil, fmt.Errorf("invalid pane width %d", width)
	}
	return []string{"resize-pane", "-t", paneID, "-x", strconv.Itoa(width)}, nil
}

// ResizePaneX sets the width of a pane.
func ResizePaneX(paneID string, width int) error {
	args, err := ResizePaneXArgs(paneID, width)
	if err != nil {
		return err
	}
	return Command(args...)
}

// UnzoomPaneArgs builds the resize-pane -Z argv. Callers must only invoke it
// when the window is zoomed, since -Z toggles the zoom state.
func UnzoomPaneArgs(paneID string) ([]string, error) {
	if !ValidPaneID(paneID) {
		return nil, ErrInvalidPaneID
	}
	return []string{"resize-pane", "-t", paneID, "-Z"}, nil
}

// UnzoomPane unzooms a zoomed window via one of its panes.
func UnzoomPane(paneID string) error {
	args, err := UnzoomPaneArgs(paneID)
	if err != nil {
		return err
	}
	return Command(args...)
}

// SetGlobalOptionArgs builds the set-option -g argv for a global option.
func SetGlobalOptionArgs(name, value string) ([]string, error) {
	if !validOptionName(name) {
		return nil, ErrInvalidOption
	}
	return []string{"set-option", "-g", name, value}, nil
}

// SetGlobalOption sets a global tmux option.
func SetGlobalOption(name, value string) error {
	args, err := SetGlobalOptionArgs(name, value)
	if err != nil {
		return err
	}
	return Command(args...)
}

// UnsetGlobalOptionArgs builds the set-option -gu argv that unsets a global
// option.
func UnsetGlobalOptionArgs(name string) ([]string, error) {
	if !validOptionName(name) {
		return nil, ErrInvalidOption
	}
	return []string{"set-option", "-gu", name}, nil
}

// UnsetGlobalOption unsets a global tmux option.
func UnsetGlobalOption(name string) error {
	args, err := UnsetGlobalOptionArgs(name)
	if err != nil {
		return err
	}
	return Command(args...)
}

// SetWindowOptionArgs builds the set-option -w argv scoped to a window id.
func SetWindowOptionArgs(windowID, name, value string) ([]string, error) {
	if !ValidWindowID(windowID) {
		return nil, ErrInvalidWindowID
	}
	if !validOptionName(name) {
		return nil, ErrInvalidOption
	}
	return []string{"set-option", "-w", "-t", windowID, name, value}, nil
}

// SetWindowOption sets a window-scoped tmux option.
func SetWindowOption(windowID, name, value string) error {
	args, err := SetWindowOptionArgs(windowID, name, value)
	if err != nil {
		return err
	}
	return Command(args...)
}

// hookTarget builds the indexed hook target `name[idx]` used by set-hook.
func hookTarget(name string, idx int) (string, error) {
	if !validHookName(name) {
		return "", ErrInvalidHookName
	}
	if idx < 0 {
		return "", fmt.Errorf("invalid hook index %d", idx)
	}
	return name + "[" + strconv.Itoa(idx) + "]", nil
}

// SetHookArgs builds the set-hook -g argv that installs cmd at name[idx].
func SetHookArgs(name string, idx int, cmd string) ([]string, error) {
	target, err := hookTarget(name, idx)
	if err != nil {
		return nil, err
	}
	return []string{"set-hook", "-g", target, cmd}, nil
}

// SetHook installs a global hook command at the given name and index.
func SetHook(name string, idx int, cmd string) error {
	args, err := SetHookArgs(name, idx, cmd)
	if err != nil {
		return err
	}
	return Command(args...)
}

// UnsetHookArgs builds the set-hook -gu argv that removes name[idx].
func UnsetHookArgs(name string, idx int) ([]string, error) {
	target, err := hookTarget(name, idx)
	if err != nil {
		return nil, err
	}
	return []string{"set-hook", "-gu", target}, nil
}

// UnsetHook removes the global hook command at the given name and index.
func UnsetHook(name string, idx int) error {
	args, err := UnsetHookArgs(name, idx)
	if err != nil {
		return err
	}
	return Command(args...)
}
