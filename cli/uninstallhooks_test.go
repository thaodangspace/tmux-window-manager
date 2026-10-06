package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const settingsWithTwm = `{
  "model": "opus",
  "hooks": {
    "Stop": [
      {"hooks": [{"type": "command", "command": "/opt/twm/bin/tmux-window-manager hook Stop"}]},
      {"hooks": [{"type": "command", "command": "say done"}]}
    ],
    "SessionStart": [
      {"hooks": [{"type": "command", "command": "'/my dir/tmux-window-manager' hook SessionStart"}]}
    ]
  }
}`

func TestRemoveClaudeHooks(t *testing.T) {
	var settings map[string]any
	if err := json.Unmarshal([]byte(settingsWithTwm), &settings); err != nil {
		t.Fatal(err)
	}
	out, events := removeClaudeHooks(settings)
	if strings.Join(events, ",") != "SessionStart,Stop" {
		t.Fatalf("events = %v", events)
	}
	hooks := asMap(out["hooks"])
	if _, ok := hooks["SessionStart"]; ok {
		t.Fatal("empty event kept")
	}
	stop := asSlice(hooks["Stop"])
	if len(stop) != 1 || groupIsTwm(stop[0]) || out["model"] != "opus" {
		t.Fatalf("foreign settings not preserved: %+v", out)
	}

	_, again := removeClaudeHooks(out)
	if len(again) != 0 {
		t.Fatalf("second pass removed %v", again)
	}
}

func TestRemoveClaudeHooksDropsEmptyHooksKey(t *testing.T) {
	settings := map[string]any{"hooks": map[string]any{
		"Stop": []any{map[string]any{"hooks": []any{map[string]any{"command": "tmux-window-manager hook Stop"}}}},
	}}
	out, _ := removeClaudeHooks(settings)
	if _, ok := out["hooks"]; ok {
		t.Fatalf("empty hooks key kept: %+v", out)
	}
}

func TestUninstallClaudeHooksWritesAndDryRun(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(settingsWithTwm), 0o600); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := uninstallClaudeHooks(&buf, path, true); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != settingsWithTwm || !strings.Contains(buf.String(), "Would remove") {
		t.Fatalf("dry run changed the file or said %q", buf.String())
	}
	buf.Reset()
	if err := uninstallClaudeHooks(&buf, path, false); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "tmux-window-manager") || !strings.Contains(string(data), "say done") {
		t.Fatalf("unexpected settings after uninstall:\n%s", data)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600 preserved", fi.Mode().Perm())
	}
	buf.Reset()
	if err := uninstallClaudeHooks(&buf, path, false); err != nil || !strings.Contains(buf.String(), "No twm hooks") {
		t.Fatalf("second run: %v %q", err, buf.String())
	}
}

func TestReportCodexNotify(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	_ = os.WriteFile(path, []byte("model = \"o3\"\nnotify = [\"/x/tmux-window-manager\", \"hook\", \"--codex\"]\n"), 0o600)
	var buf bytes.Buffer
	reportCodexNotify(&buf, path)
	if !strings.Contains(buf.String(), "line 2") {
		t.Fatalf("report = %q", buf.String())
	}
}
