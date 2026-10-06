package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/thaodangspace/tmux-window-manager/notify"
)

func TestMacOSSenderSpawnsWaiter(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "alerter"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("TMPDIR", t.TempDir())
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	var spawned []string
	m := &macosSender{
		cfg:   notify.MacOSConfig{TerminalBundleID: "com.googlecode.iterm2"},
		spawn: func(argv []string) error { spawned = argv; return nil },
	}
	event := notify.Event{Kind: notify.Waiting, Agent: "claude", SessionID: "s", Pane: "%7", TmuxSocket: "/tmp/sock"}
	if err := m.Notify(context.Background(), event); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	head := []string{self, "notify-wait", "--group", "twm-claude-s", "--pane", "%7", "--socket", "/tmp/sock", "--activate", "com.googlecode.iterm2", "--", filepath.Join(bin, "alerter")}
	if len(spawned) < len(head) || !reflect.DeepEqual(spawned[:len(head)], head) {
		t.Fatalf("spawned %q\nwant prefix %q", spawned, head)
	}

	// Without a canonical pane there is nothing to jump to.
	event.Pane = "work:1"
	_ = m.Notify(context.Background(), event)
	if strings.Contains(strings.Join(spawned, " "), "--pane") {
		t.Fatalf("invalid pane forwarded: %q", spawned)
	}

	m.spawn = func([]string) error { return errors.New("fork failed") }
	if err := m.Notify(context.Background(), event); !errors.Is(err, notify.ErrNotifierFailed) {
		t.Fatalf("spawn failure err = %v", err)
	}
}

func TestNotifyWait(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	var jumped, activated []string
	prevRun, prevJump, prevActivate := waitRun, waitJump, waitActivate
	t.Cleanup(func() { waitRun, waitJump, waitActivate = prevRun, prevJump, prevActivate })
	waitJump = func(pane string) error { jumped = append(jumped, pane); return nil }
	waitActivate = func(bundle string) error { activated = append(activated, bundle); return nil }

	output := `{"activationType":"contentsClicked"}`
	waitRun = func(_ context.Context, argv []string) ([]byte, error) {
		if !reflect.DeepEqual(argv, []string{"alerter", "--json"}) {
			t.Errorf("ran %q", argv)
		}
		// The waiter is discoverable (for replacement) while alerter runs.
		if _, err := os.Stat(waiterPIDFile("g")); err != nil {
			t.Errorf("pid file missing during wait: %v", err)
		}
		return []byte(output), nil
	}

	notifyWait("g", "%3", "", "com.mitchellh.ghostty", []string{"alerter", "--json"})
	if !reflect.DeepEqual(jumped, []string{"%3"}) || !reflect.DeepEqual(activated, []string{"com.mitchellh.ghostty"}) {
		t.Fatalf("click: jumped=%q activated=%q", jumped, activated)
	}
	if _, err := os.Stat(waiterPIDFile("g")); !os.IsNotExist(err) {
		t.Fatalf("pid file left behind: %v", err)
	}

	output = `{"activationType":"timeout"}`
	notifyWait("g", "%3", "", "com.mitchellh.ghostty", []string{"alerter", "--json"})
	if len(jumped) != 1 || len(activated) != 1 {
		t.Fatalf("timeout acted: jumped=%q activated=%q", jumped, activated)
	}
}

func TestStopWaiterIgnoresForeignPID(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	// Our own pid does not run notify-wait, so it must not be signalled.
	if err := os.WriteFile(waiterPIDFile("g"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		t.Fatal(err)
	}
	stopWaiter("g")
	if _, err := os.Stat(waiterPIDFile("g")); !os.IsNotExist(err) {
		t.Fatalf("stale pid file kept: %v", err)
	}
}
