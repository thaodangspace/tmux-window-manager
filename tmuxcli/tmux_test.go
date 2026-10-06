package tmuxcli

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestParseClients(t *testing.T) {
	got := parseClients("/dev/ttys000\t100\n/dev/ttys007\t200\ninvalid\n/dev/ttys-bad\tnope\n\t300\n")
	want := []Client{
		{Name: "/dev/ttys000", Activity: 100},
		{Name: "/dev/ttys007", Activity: 200},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseClients() = %#v, want %#v", got, want)
	}
}

func TestMostRecentClient(t *testing.T) {
	tests := []struct {
		name    string
		clients []Client
		want    Client
		found   bool
	}{
		{name: "empty"},
		{
			name: "greatest activity",
			clients: []Client{
				{Name: "older", Activity: 10},
				{Name: "newer", Activity: 20},
			},
			want:  Client{Name: "newer", Activity: 20},
			found: true,
		},
		{
			name: "name tie breaker",
			clients: []Client{
				{Name: "zsh", Activity: 20},
				{Name: "bash", Activity: 20},
			},
			want:  Client{Name: "bash", Activity: 20},
			found: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, found := MostRecentClient(tt.clients)
			if got != tt.want || found != tt.found {
				t.Fatalf("MostRecentClient() = %#v, %v; want %#v, %v", got, found, tt.want, tt.found)
			}
		})
	}
}

func TestValidPaneID(t *testing.T) {
	for _, tt := range []struct {
		value string
		valid bool
	}{
		{value: "%0", valid: true},
		{value: "%20", valid: true},
		{value: "", valid: false},
		{value: "%", valid: false},
		{value: "20", valid: false},
		{value: "%20:1", valid: false},
		{value: "%20/1", valid: false},
		{value: "%x", valid: false},
	} {
		t.Run(tt.value, func(t *testing.T) {
			if got := ValidPaneID(tt.value); got != tt.valid {
				t.Fatalf("ValidPaneID(%q) = %v, want %v", tt.value, got, tt.valid)
			}
		})
	}
}

func TestSwitchClientArgs(t *testing.T) {
	got, err := SwitchClientArgs("/dev/tty with space", "%20")
	if err != nil {
		t.Fatalf("SwitchClientArgs() error = %v", err)
	}
	want := []string{"switch-client", "-c", "/dev/tty with space", "-t", "%20"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SwitchClientArgs() = %#v, want %#v", got, want)
	}

	for _, tt := range []struct {
		name   string
		client string
		pane   string
		want   error
	}{
		{name: "empty client", pane: "%20", want: errors.New("invalid tmux client name")},
		{name: "newline client", client: "tty\nname", pane: "%20", want: errors.New("invalid tmux client name")},
		{name: "invalid pane", client: "tty", pane: "session:window", want: ErrInvalidPaneID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SwitchClientArgs(tt.client, tt.pane)
			if err == nil || err.Error() != tt.want.Error() {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
			if errors.Is(tt.want, ErrInvalidPaneID) && !errors.Is(err, ErrInvalidPaneID) {
				t.Fatalf("error = %v, want ErrInvalidPaneID", err)
			}
		})
	}
}

func TestParsePaneFocus(t *testing.T) {
	tests := []struct {
		out  string
		want paneFocusInfo
		ok   bool
	}{
		{out: "$1\twork\t3\t1\t1\n", want: paneFocusInfo{sessionID: "$1", location: "work:3", visible: true}, ok: true},
		{out: "$1\twork\t3\t0\t1\n", want: paneFocusInfo{sessionID: "$1", location: "work:3"}, ok: true},
		{out: "$1\twork\t3\t1\t0", want: paneFocusInfo{sessionID: "$1", location: "work:3"}, ok: true},
		{out: "$1\t\t3\t1\t1"},
		{out: "garbage"},
		{out: ""},
	}
	for _, tt := range tests {
		got, ok := parsePaneFocus(tt.out)
		if ok != tt.ok || got != tt.want {
			t.Errorf("parsePaneFocus(%q) = %+v, %v; want %+v, %v", tt.out, got, ok, tt.want, tt.ok)
		}
	}
}

func TestRecentlyActive(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	for out, want := range map[string]bool{
		"":                 false,
		"999000\n":         false, // 1000s ago
		"999000\n999950\n": true,  // one client 50s ago
		"1000000":          true,
		"nope\n0\n-5\n":    false,
	} {
		if got := recentlyActive(out, now, 2*time.Minute); got != want {
			t.Errorf("recentlyActive(%q) = %v, want %v", out, got, want)
		}
	}
}

func TestLookupPaneRejectsInvalidID(t *testing.T) {
	if _, ok := LookupPane("", time.Now(), time.Minute); ok {
		t.Fatal("empty pane id resolved")
	}
	if _, ok := LookupPane("work:1", time.Now(), time.Minute); ok {
		t.Fatal("non-canonical target resolved")
	}
}

func TestSocketFromEnv(t *testing.T) {
	for in, want := range map[string]string{
		"":                                    "",
		"/private/tmp/tmux-501/default,123,0": "/private/tmp/tmux-501/default",
		"/tmp/tmux-501/work":                  "/tmp/tmux-501/work",
	} {
		if got := SocketFromEnv(in); got != want {
			t.Errorf("SocketFromEnv(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTmuxCmdSocket(t *testing.T) {
	prev := Socket
	t.Cleanup(func() { Socket = prev })

	Socket = ""
	if got := tmuxCmd("list-clients").Args[1:]; !reflect.DeepEqual(got, []string{"list-clients"}) {
		t.Fatalf("args without socket = %q", got)
	}
	Socket = "/tmp/s"
	if got := tmuxCmd("list-clients").Args[1:]; !reflect.DeepEqual(got, []string{"-S", "/tmp/s", "list-clients"}) {
		t.Fatalf("args with socket = %q", got)
	}
}
