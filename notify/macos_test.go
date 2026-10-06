package notify

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLoadMacOSConfig(t *testing.T) {
	env := map[string]string{"__CFBundleIdentifier": "com.mitchellh.ghostty"}
	tests := []struct {
		name     string
		contents string
		env      map[string]string
		want     MacOSConfig
		enabled  bool
		wantErr  error
	}{
		{name: "missing section is disabled", contents: "[telegram]\n"},
		{name: "explicitly disabled", contents: "[macos]\nenabled = false\n"},
		{
			name:     "defaults use launching terminal",
			contents: "[macos]\nenabled = true\n",
			env:      env,
			want:     MacOSConfig{TerminalBundleID: "com.mitchellh.ghostty", Sound: "default", TimeoutSeconds: defaultMacOSTimeout, Options: DefaultOptions()},
			enabled:  true,
		},
		{
			name:     "all keys set",
			contents: "[macos]\nenabled = true\nterminal_bundle_id = \" com.googlecode.iterm2 \"\nsound = \"\"\ntimeout_seconds = 0\ninclude_response = true\nmin_turn_seconds = 0\nskip_when_focused = false\n",
			env:      env,
			want:     MacOSConfig{TerminalBundleID: "com.googlecode.iterm2", Options: Options{IncludeResponse: true}},
			enabled:  true,
		},
		{
			name:     "telegram options do not leak into macos",
			contents: "[telegram]\nmin_turn_seconds = 5\n[macos]\nenabled = true\n",
			want:     MacOSConfig{Sound: "default", TimeoutSeconds: defaultMacOSTimeout, Options: DefaultOptions()},
			enabled:  true,
		},
		{name: "malformed file", contents: "[macos\n", wantErr: ErrConfigFile},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), configFileName)
			if err := os.WriteFile(path, []byte(tt.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			got, enabled, err := loadMacOSConfig(path, func(k string) string { return tt.env[k] })
			if !errors.Is(err, tt.wantErr) || enabled != tt.enabled || got != tt.want {
				t.Fatalf("loadMacOSConfig() = %+v, %v, %v; want %+v, %v, %v", got, enabled, err, tt.want, tt.enabled, tt.wantErr)
			}
		})
	}
}

func TestComposePlain(t *testing.T) {
	waiting, ok := ComposePlain(Event{
		Kind: Waiting, Agent: "claude", Cwd: "/work/project", Location: "work:3",
		Prompt: "fix\nthe bug", Detail: "permission required",
	})
	if !ok || waiting != (Plain{
		Title:    "🔔 Claude needs input",
		Subtitle: "project · work:3",
		Body:     "permission required\nPrompt: fix the bug",
	}) {
		t.Fatalf("waiting = %+v, %v", waiting, ok)
	}

	done, _ := ComposePlain(Event{
		Kind: Completed, Agent: "codex", Cwd: "/work/project",
		Duration: 4*time.Minute + 12*time.Second, Response: "all green",
	})
	if done != (Plain{
		Title:    "✅ Codex finished",
		Subtitle: "project · 4m12s",
		Body:     "all green",
	}) {
		t.Fatalf("completed = %+v", done)
	}

	if _, ok := ComposePlain(Event{Kind: "other"}); ok {
		t.Fatal("unknown kind composed")
	}
}

func TestMacOSGroup(t *testing.T) {
	if got := MacOSGroup(Event{Agent: "claude", SessionID: "ab/c d-1"}); got != "twm-claude-ab_c_d-1" {
		t.Fatalf("MacOSGroup = %q", got)
	}
}

func TestAlerterArgv(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, alerterBin), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	cfg := MacOSConfig{Sound: "Glass", TimeoutSeconds: 60}

	got, err := AlerterArgv(cfg, Event{Kind: Waiting, Agent: "claude", SessionID: "s", Cwd: "/w/-rf", Detail: "-x"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(bin, alerterBin),
		"--title=🔔 Claude needs input",
		"--subtitle=-rf",
		"--message=-x",
		"--group=twm-claude-s",
		"--timeout=60",
		"--json",
		"--sound=Glass",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("argv =\n%q\nwant\n%q", got, want)
	}

	// Completed turns are silent.
	quiet, _ := AlerterArgv(cfg, Event{Kind: Completed, Agent: "claude"})
	if strings.Contains(strings.Join(quiet, " "), "--sound") {
		t.Fatalf("completed argv has a sound: %q", quiet)
	}
	if _, err := AlerterArgv(cfg, Event{Kind: "other"}); !errors.Is(err, ErrRequest) {
		t.Fatalf("unknown kind err = %v", err)
	}
	t.Setenv("PATH", t.TempDir())
	if _, err := AlerterArgv(cfg, Event{Kind: Waiting}); !errors.Is(err, ErrNotifierMissing) {
		t.Fatalf("missing alerter err = %v", err)
	}
}

func TestClicked(t *testing.T) {
	for out, want := range map[string]bool{
		`{"activationType":"contentsClicked","deliveredAt":"x"}`:      true,
		`{"activationType":"actionClicked","activationValue":"Open"}`: true,
		`{"activationType":"timeout"}`:                                false,
		`{"activationType":"closed"}`:                                 false,
		`@CONTENTCLICKED`:                                             false,
		``:                                                            false,
	} {
		if got := Clicked([]byte(out)); got != want {
			t.Errorf("Clicked(%q) = %v, want %v", out, got, want)
		}
	}
}
