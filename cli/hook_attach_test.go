package cli

import (
	"errors"
	"strings"
	"testing"

	"github.com/thaodangspace/tmux-window-manager/store"
)

type fakeHookAttach struct {
	url       string
	commits   int
	cancels   int
	commitErr error
}

func (f *fakeHookAttach) URL() string { return f.url }
func (f *fakeHookAttach) Commit() error {
	f.commits++
	return f.commitErr
}
func (f *fakeHookAttach) Cancel() error {
	f.cancels++
	return nil
}

func TestHookAttachLinkIsPreparedAfterDBWriteAndCommittedAfterSend(t *testing.T) {
	db := withTempDB(t)
	if err := db.Upsert(store.Status{Agent: "claude", SessionID: "attach", Cwd: "/work/project", Status: store.Running, Prompt: "continue", UpdatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_PANE", "%20")
	link := &fakeHookAttach{url: "http://127.0.0.1:49152/attach/opaque-token-1234567890"}
	sender := &fakeHookNotifier{}
	var target string
	starter := func(got string) (hookAttach, error) {
		target = got
		rows, err := db.All()
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 1 || rows[0].Status != store.Waiting {
			t.Fatalf("attach started before DB write: %+v", rows)
		}
		return link, nil
	}

	runHookWithNotifierAndAttach("claude", "Notification", false,
		[]byte(`{"session_id":"attach","cwd":"/work/project","message":"need input"}`),
		func() (hookNotifier, bool, error) { return sender, true, nil }, starter)

	if target != "%20" || len(sender.messages) != 1 || !strings.Contains(sender.messages[0], "[Attach in tmux](http://127.0.0.1:49152/attach/opaque-token-1234567890)") {
		t.Fatalf("target/message = %q/%q", target, sender.messages)
	}
	if link.commits != 1 || link.cancels != 0 {
		t.Fatalf("link lifecycle = commits %d cancels %d", link.commits, link.cancels)
	}
}

func TestHookAttachFailureFallsBackAndCancelsOnTelegramFailure(t *testing.T) {
	t.Run("startup failure sends without link", func(t *testing.T) {
		withTempDB(t)
		t.Setenv("TMUX_PANE", "%20")
		sender := &fakeHookNotifier{}
		runHookWithNotifierAndAttach("claude", "Stop", false,
			[]byte(`{"session_id":"s","cwd":"/work"}`),
			func() (hookNotifier, bool, error) { return sender, true, nil },
			func(string) (hookAttach, error) { return nil, errors.New("startup failed") })
		if len(sender.messages) != 1 || strings.Contains(sender.messages[0], "Attach in tmux") {
			t.Fatalf("fallback message = %q", sender.messages)
		}
	})

	t.Run("delivery failure cancels helper", func(t *testing.T) {
		withTempDB(t)
		t.Setenv("TMUX_PANE", "%20")
		link := &fakeHookAttach{url: "http://127.0.0.1:49152/attach/opaque-token-1234567890"}
		runHookWithNotifierAndAttach("claude", "Notification", false,
			[]byte(`{"session_id":"s","cwd":"/work","message":"input"}`),
			func() (hookNotifier, bool, error) {
				return &fakeHookNotifier{err: errors.New("send failed")}, true, nil
			},
			func(string) (hookAttach, error) { return link, nil })
		if link.cancels != 1 || link.commits != 0 {
			t.Fatalf("link lifecycle = commits %d cancels %d", link.commits, link.cancels)
		}
	})
}

func TestHookAttachDoesNotStartWhenTelegramDisabledOrIneligible(t *testing.T) {
	for _, tt := range []struct {
		name  string
		agent string
		event string
		codex bool
		raw   string
	}{
		{name: "disabled", agent: "claude", event: "Notification", raw: `{"session_id":"s","cwd":"/work","message":"input"}`},
		{name: "session start", agent: "claude", event: "SessionStart", raw: `{"session_id":"s","cwd":"/work"}`},
		{name: "codex", agent: "codex", codex: true, raw: `{"type":"agent-turn-complete","thread-id":"s","cwd":"/work"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			withTempDB(t)
			t.Setenv("TMUX_PANE", "%20")
			starts := 0
			factory := func() (hookNotifier, bool, error) {
				return &fakeHookNotifier{}, false, nil
			}
			starter := func(string) (hookAttach, error) {
				starts++
				return &fakeHookAttach{}, nil
			}
			runHookWithNotifierAndAttach(tt.agent, tt.event, tt.codex, []byte(tt.raw), factory, starter)
			if starts != 0 {
				t.Fatalf("attach starts = %d, want 0", starts)
			}
		})
	}
}
