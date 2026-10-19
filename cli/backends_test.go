package cli

import (
	"context"
	"testing"
	"time"

	"github.com/thaodangspace/tmux-window-manager/notify"
)

func TestDeliverRoutesByPresence(t *testing.T) {
	withWhen := func(when string) notify.Options {
		opts := notify.DefaultOptions()
		opts.When = when
		return opts
	}
	tests := []struct {
		name                   string
		presence               notify.Presence
		err                    error
		macos, telegram, plain int
	}{
		{name: "active", presence: notify.Presence{Idle: time.Second}, macos: 1, plain: 1},
		{name: "idle", presence: notify.Presence{Idle: 10 * time.Minute}, telegram: 1, plain: 1},
		{name: "locked", presence: notify.Presence{Idle: time.Second, Locked: true}, telegram: 1, plain: 1},
		{name: "unknown sends everywhere", err: notify.ErrPresence, macos: 1, telegram: 1, plain: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reads := 0
			old := readPresence
			readPresence = func(context.Context) (notify.Presence, error) { reads++; return tt.presence, tt.err }
			t.Cleanup(func() { readPresence = old })

			macos, telegram, plain := &fakeNotifier{}, &fakeNotifier{}, &fakeNotifier{}
			deliver([]backend{
				{name: "macos", sender: macos, opts: withWhen(notify.WhenActive)},
				{name: "telegram", sender: telegram, opts: withWhen(notify.WhenAway)},
				{name: "plain", sender: plain, opts: notify.DefaultOptions()},
			}, notify.Event{Kind: notify.Waiting}, false, "", nil)

			if len(macos.events) != tt.macos || len(telegram.events) != tt.telegram || len(plain.events) != tt.plain {
				t.Fatalf("macos=%d telegram=%d plain=%d, want %d %d %d",
					len(macos.events), len(telegram.events), len(plain.events), tt.macos, tt.telegram, tt.plain)
			}
			if reads != 1 {
				t.Fatalf("presence read %d times, want 1", reads)
			}
		})
	}
}

func TestDeliverSkipsPresenceProbeWithoutWhen(t *testing.T) {
	old := readPresence
	readPresence = func(context.Context) (notify.Presence, error) {
		t.Fatal("presence read without a `when` backend")
		return notify.Presence{}, nil
	}
	t.Cleanup(func() { readPresence = old })
	sender := &fakeNotifier{}
	deliver([]backend{{name: "plain", sender: sender, opts: notify.DefaultOptions()}}, notify.Event{Kind: notify.Waiting}, false, "", nil)
	if len(sender.events) != 1 {
		t.Fatalf("events = %d, want 1", len(sender.events))
	}
}
