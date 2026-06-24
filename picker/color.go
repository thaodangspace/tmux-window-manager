package picker

// ANSI palette — identical to the original tmux_window_manager.sh so the fzf
// list looks byte-for-byte the same.
const (
	Cyan   = "\033[1;36m"
	Green  = "\033[32m"
	Dim    = "\033[2m"
	Ylw    = "\033[33m"
	Italic = "\033[3m"
	Rst    = "\033[0m"

	// Robot suffix = an AI coding agent is present in the window/session.
	// Status is rendered as italicized text rather than a glyph: "running"
	// (cyan) when the agent is working, "waiting" (yellow) when it is blocked
	// on the user and wants attention. Idle shows nothing.
	Robot = " " + Ylw + "🤖" + Rst
)
