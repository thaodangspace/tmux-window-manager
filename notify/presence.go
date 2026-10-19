package notify

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"time"
)

// Presence is whether the user is at this Mac: how long since the last
// keyboard/mouse input, and whether the screen is locked.
type Presence struct {
	Idle   time.Duration
	Locked bool
}

// ErrPresence means the user's presence could not be read.
var ErrPresence = errors.New("presence unavailable")

// Away reports whether p counts as away under opts: the screen is locked or
// there has been no input for opts.AwayAfter.
func (p Presence) Away(opts Options) bool {
	return p.Locked || p.Idle >= opts.AwayAfter
}

var (
	hidIdleRe = regexp.MustCompile(`"HIDIdleTime" = (\d+)`)
	lockedRe  = regexp.MustCompile(`"CGSSessionScreenIsLocked"=Yes`)
)

// ReadPresence asks ioreg for the HID idle time (nanoseconds) and the console
// session's lock flag. Only darwin is supported.
func ReadPresence(ctx context.Context) (Presence, error) {
	if runtime.GOOS != "darwin" {
		return Presence{}, ErrPresence
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	hid, err := exec.CommandContext(ctx, "ioreg", "-c", "IOHIDSystem", "-d", "4").Output()
	if err != nil {
		return Presence{}, ErrPresence
	}
	root, err := exec.CommandContext(ctx, "ioreg", "-n", "Root", "-d", "1").Output()
	if err != nil {
		return Presence{}, ErrPresence
	}
	return parsePresence(string(hid), string(root))
}

func parsePresence(hid, root string) (Presence, error) {
	m := hidIdleRe.FindStringSubmatch(hid)
	if m == nil {
		return Presence{}, ErrPresence
	}
	ns, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return Presence{}, ErrPresence
	}
	return Presence{Idle: time.Duration(ns), Locked: lockedRe.MatchString(root)}, nil
}
