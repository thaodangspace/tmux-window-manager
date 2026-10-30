package cli

import (
	"bytes"
	"errors"
	"testing"

	"github.com/thaodangspace/tmux-window-manager/notify"
)

func TestNotifyTest(t *testing.T) {
	var out bytes.Buffer
	if err := notifyTest(&out, nil, notify.Event{}); err != nil || !bytes.Contains(out.Bytes(), []byte("no notification backend")) {
		t.Fatalf("no backends: err=%v out=%q", err, out.String())
	}

	// Filters are bypassed: a focused-only backend still gets the event.
	sent := &fakeNotifier{}
	backends := []backend{
		{name: "telegram", err: notify.ErrPartialConfig},
		{name: "macos", sender: sent, opts: notify.Options{SkipWhenFocused: true, When: notify.WhenAway}},
	}
	out.Reset()
	err := notifyTest(&out, backends, notify.Event{Kind: notify.Waiting})
	if !errors.Is(err, errNotifyTestFailed) {
		t.Fatalf("err = %v, want a failure", err)
	}
	want := "telegram: failed (partial-config) — set both bot_token and chat_id\nmacos: sent\n"
	if out.String() != want || len(sent.events) != 1 {
		t.Fatalf("out = %q events=%d\nwant %q", out.String(), len(sent.events), want)
	}
}
