package notify

import (
	"errors"
	"testing"
	"time"
)

func TestParsePresence(t *testing.T) {
	hid := `    | |   "HIDIdleTime" = 13734462125` + "\n"
	got, err := parsePresence(hid, `"IOConsoleUsers" = ({"kCGSSessionOnConsoleKey"=Yes})`)
	if err != nil || got != (Presence{Idle: 13734462125 * time.Nanosecond}) {
		t.Fatalf("unlocked = %+v, %v", got, err)
	}
	got, err = parsePresence(hid, `({"CGSSessionScreenIsLocked"=Yes,"kCGSSessionOnConsoleKey"=Yes})`)
	if err != nil || !got.Locked {
		t.Fatalf("locked = %+v, %v", got, err)
	}
	if _, err := parsePresence("no idle here", ""); !errors.Is(err, ErrPresence) {
		t.Fatalf("missing idle err = %v, want ErrPresence", err)
	}
}

func TestPresenceAway(t *testing.T) {
	opts := DefaultOptions()
	for _, tt := range []struct {
		p    Presence
		away bool
	}{
		{Presence{Idle: time.Minute}, false},
		{Presence{Idle: 5 * time.Minute}, true},
		{Presence{Idle: time.Second, Locked: true}, true},
	} {
		if got := tt.p.Away(opts); got != tt.away {
			t.Errorf("%+v.Away() = %v, want %v", tt.p, got, tt.away)
		}
	}
}
