package cli

import (
	"os"
	"path/filepath"
	"testing"
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
