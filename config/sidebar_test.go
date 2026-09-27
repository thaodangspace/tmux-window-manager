package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "twm.toml")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadFrom(t *testing.T) {
	def := DefaultSidebar()

	tests := []struct {
		name    string
		write   bool
		body    string
		want    Sidebar
		wantErr bool
	}{
		{name: "missing file", write: false, want: def},
		{name: "empty file", write: true, body: "", want: def},
		{
			name:  "telegram only",
			write: true,
			body:  "[telegram]\nbot_token = \"abc\"\nchat_id = \"123\"\n",
			want:  def,
		},
		{
			name:  "full sidebar",
			write: true,
			body:  "[sidebar]\nenabled = true\nwidth = 40\nrefresh_ms = 2000\n",
			want:  Sidebar{Enabled: true, Width: 40, RefreshMS: 2000},
		},
		{
			name:  "clamp width low",
			write: true,
			body:  "[sidebar]\nwidth = 5\n",
			want:  Sidebar{Enabled: true, Width: 20, RefreshMS: 1000},
		},
		{
			name:  "clamp width high",
			write: true,
			body:  "[sidebar]\nwidth = 999\n",
			want:  Sidebar{Enabled: true, Width: 80, RefreshMS: 1000},
		},
		{
			name:  "clamp refresh low",
			write: true,
			body:  "[sidebar]\nrefresh_ms = 100\n",
			want:  Sidebar{Enabled: true, Width: 32, RefreshMS: 500},
		},
		{
			name:  "clamp refresh high",
			write: true,
			body:  "[sidebar]\nrefresh_ms = 60000\n",
			want:  Sidebar{Enabled: true, Width: 32, RefreshMS: 10000},
		},
		{
			name:  "enabled false",
			write: true,
			body:  "[sidebar]\nenabled = false\n",
			want:  Sidebar{Enabled: false, Width: 32, RefreshMS: 1000},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var path string
			if tc.write {
				path = writeFile(t, tc.body)
			} else {
				path = filepath.Join(t.TempDir(), "does-not-exist.toml")
			}
			got, err := LoadFrom(path)
			if tc.wantErr != (err != nil) {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestLoadFromMalformedRedacted(t *testing.T) {
	const secret = "SUPERSECRETTOKEN12345"
	// Unterminated string keeps the secret in the file but breaks parsing.
	path := writeFile(t, "[telegram]\nbot_token = \""+secret+"\n[sidebar\n")

	got, err := LoadFrom(path)
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("err = %v, want ErrConfig", err)
	}
	if got != DefaultSidebar() {
		t.Fatalf("got %+v, want defaults", got)
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error text leaked secret: %q", err.Error())
	}
}

func TestLoadFromUnreadableDir(t *testing.T) {
	// A directory (not a regular file) is not IsNotExist, so os.ReadFile
	// returns a non-parse read error: defaults + ErrConfig, no leak.
	dir := t.TempDir()

	got, err := LoadFrom(dir)
	if !errors.Is(err, ErrConfig) {
		t.Fatalf("err = %v, want ErrConfig", err)
	}
	if got != DefaultSidebar() {
		t.Fatalf("got %+v, want defaults", got)
	}
}

func TestLoadUsesXDGPath(t *testing.T) {
	xdg := t.TempDir()
	path := filepath.Join(xdg, configFileName)
	if err := os.WriteFile(path, []byte("[sidebar]\nwidth = 40\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", xdg)

	got, consulted, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Width != 40 {
		t.Fatalf("got width %d, want 40", got.Width)
	}
	if consulted != path {
		t.Fatalf("consulted %q, want %q", consulted, path)
	}
}

func TestPathXDGBeatsHome(t *testing.T) {
	xdg := t.TempDir()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Setenv("HOME", home)

	got, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if want := filepath.Join(xdg, configFileName); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	t.Setenv("XDG_CONFIG_HOME", "")
	got, err = Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if want := filepath.Join(home, ".config", configFileName); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestResolveEnabled(t *testing.T) {
	tests := []struct {
		runtime string
		file    bool
		want    bool
	}{
		{"1", false, true},
		{"1", true, true},
		{"0", true, false},
		{"0", false, false},
		{"", true, true},
		{"", false, false},
	}
	for _, tc := range tests {
		got := ResolveEnabled(tc.runtime, Sidebar{Enabled: tc.file})
		if got != tc.want {
			t.Fatalf("ResolveEnabled(%q, %v) = %v, want %v", tc.runtime, tc.file, got, tc.want)
		}
	}
}
