package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
)

func TestResolvePath(t *testing.T) {
	home, _ := os.UserHomeDir()
	base := "/work/here"
	tests := []struct {
		in, want string
	}{
		{"/abs/path", "/abs/path"},
		{"relative", filepath.Join(base, "relative")},
		{"sub/dir", filepath.Join(base, "sub/dir")},
		{"~", home},
		{"~/projects", filepath.Join(home, "projects")},
	}
	for _, tt := range tests {
		if got := resolvePath(tt.in, base); got != tt.want {
			t.Errorf("resolvePath(%q, %q) = %q, want %q", tt.in, base, got, tt.want)
		}
	}
}

func TestKillSelectedWindowCountsUsingWindowTarget(t *testing.T) {
	if tmuxcli.Socket != "" {
		t.Fatal("test requires the default tmux socket")
	}
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	fake := `#!/bin/sh
printf '%s\n' "$*" >> "$TWM_TEST_CALLS"
case "$*" in
  'display-message -p -t =beta:2 #{session_windows}') printf '2\n' ;;
  'kill-window -t =beta:2') ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TWM_TEST_CALLS", log)
	if err := killSelected("client", "beta:2"); err != nil {
		t.Fatal(err)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	want := "display-message -p -t =beta:2 #{session_windows}\nkill-window -t =beta:2\n"
	if string(calls) != want {
		t.Fatalf("tmux calls = %q, want %q", calls, want)
	}
}

func TestKillWindowCommand(t *testing.T) {
	for _, tt := range []struct {
		count string
		ok    bool
	}{{"2", true}, {"1", false}, {"", false}, {"invalid", false}} {
		got, ok := killWindowCommand("beta:2", tt.count)
		if ok != tt.ok {
			t.Errorf("count %q: ok = %v", tt.count, ok)
		}
		if ok && !reflect.DeepEqual(got, []string{"kill-window", "-t", "=beta:2"}) {
			t.Errorf("count %q: args = %v", tt.count, got)
		}
	}
}

func TestKillSessionCommand(t *testing.T) {
	sessions := []string{"alpha", "beta", "gamma"}
	tests := []struct {
		name, client, target, current, last string
		sessions                            []string
		want                                []string
		ok                                  bool
	}{
		{name: "other session header", client: "/dev/ttys3", target: "beta", current: "alpha", sessions: sessions,
			want: []string{"kill-session", "-t", "=beta"}, ok: true},
		{name: "current session switches to last first", client: "/dev/ttys3", target: "beta", current: "beta", last: "gamma", sessions: sessions,
			want: []string{"switch-client", "-c", "/dev/ttys3", "-t", "=gamma", ";", "kill-session", "-t", "=beta"}, ok: true},
		{name: "current session without last uses first other", target: "beta", current: "beta", sessions: sessions,
			want: []string{"switch-client", "-t", "=alpha", ";", "kill-session", "-t", "=beta"}, ok: true},
		{name: "stale last session ignored", client: "c", target: "beta", current: "beta", last: "gone", sessions: sessions,
			want: []string{"switch-client", "-c", "c", "-t", "=alpha", ";", "kill-session", "-t", "=beta"}, ok: true},
		{name: "last remaining session refused", client: "c", target: "alpha", current: "alpha", sessions: []string{"alpha"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := killSessionCommand(tt.client, tt.target, tt.current, tt.last, tt.sessions)
			if ok != tt.ok || !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %v, %v; want %v, %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestSwitchCommand(t *testing.T) {
	tests := []struct {
		name           string
		client, target string
		want           []string
	}{
		{"window row with client", "/dev/ttys3", "beta:2",
			[]string{"switch-client", "-c", "/dev/ttys3", "-t", "beta", ";", "select-window", "-t", "beta:2"}},
		{"header row with client", "/dev/ttys3", "beta",
			[]string{"switch-client", "-c", "/dev/ttys3", "-t", "beta"}},
		{"window row no client", "", "alpha:1",
			[]string{"switch-client", "-t", "alpha", ";", "select-window", "-t", "alpha:1"}},
		{"header row no client", "", "alpha",
			[]string{"switch-client", "-t", "alpha"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := switchCommand(tt.client, tt.target)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("arg %d: got %q want %q\nfull: %v", i, got[i], tt.want[i], got)
				}
			}
		})
	}
}

func TestDeriveSessionName(t *testing.T) {
	tests := []struct {
		query, dir, want string
	}{
		{"myproj", "/x/y", "myproj"},
		{"", "/x/y/cool-app", "cool-app"},
		{"has:colon", "/x", "has-colon"},
		{"", "/x/a:b", "a-b"},
	}
	for _, tt := range tests {
		if got := deriveSessionName(tt.query, tt.dir); got != tt.want {
			t.Errorf("deriveSessionName(%q,%q)=%q want %q", tt.query, tt.dir, got, tt.want)
		}
	}
}

func TestLineAt(t *testing.T) {
	lines := []string{"query", "ctrl-n", "sel"}
	if lineAt(lines, 0) != "query" || lineAt(lines, 2) != "sel" {
		t.Error("lineAt returned wrong values")
	}
	if lineAt(lines, 5) != "" {
		t.Error("lineAt out of range should be empty")
	}
}
