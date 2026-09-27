package picker

// ANSI palette — identical to the original tmux_window_manager.sh so the fzf
// list looks byte-for-byte the same.
const (
	Cyan   = "\033[1;36m"
	Green  = "\033[32m"
	Red    = "\033[31m"
	Dim    = "\033[2m"
	Ylw    = "\033[33m"
	Italic = "\033[3m"
	Rst    = "\033[0m"

	// Robot marks an AI coding agent in the window/session.
	Robot = " " + Ylw + "🤖" + Rst
)
