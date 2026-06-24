package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const bin = "/opt/twm/tmux-window-manager"

func TestMergeAddsAllEventsAndIsIdempotent(t *testing.T) {
	settings := map[string]any{}
	first := mergeClaudeHooks(settings, bin)

	hooks := asMap(first["hooks"])
	for _, ev := range claudeHookEvents {
		groups := asSlice(hooks[ev])
		if len(groups) != 1 {
			t.Fatalf("event %s: want 1 group, got %d", ev, len(groups))
		}
		cmd := commandOf(t, groups[0])
		if !strings.Contains(cmd, "hook "+ev) {
			t.Fatalf("event %s: command = %q", ev, cmd)
		}
	}

	// Idempotent: serialize, re-merge, compare bytes.
	b1, _ := marshalSettings(first)
	var reparsed map[string]any
	json.Unmarshal(b1, &reparsed)
	b2, _ := marshalSettings(mergeClaudeHooks(reparsed, bin))
	if !bytes.Equal(b1, b2) {
		t.Fatalf("not idempotent:\nfirst:\n%s\nsecond:\n%s", b1, b2)
	}
}

func TestMergePreservesForeignHooksAndSettings(t *testing.T) {
	settings := map[string]any{
		"theme": "dark",
		"hooks": map[string]any{
			"PostToolUseFailure": []any{
				map[string]any{
					"matcher": "Bash",
					"hooks":   []any{map[string]any{"type": "command", "command": "/foo/nono.sh"}},
				},
			},
			// A user's own Notification hook must survive alongside ours.
			"Notification": []any{
				map[string]any{
					"hooks": []any{map[string]any{"type": "command", "command": "/usr/bin/say hi"}},
				},
			},
		},
	}
	out := mergeClaudeHooks(settings, bin)

	if out["theme"] != "dark" {
		t.Errorf("unrelated setting lost")
	}
	hooks := asMap(out["hooks"])
	if len(asSlice(hooks["PostToolUseFailure"])) != 1 {
		t.Errorf("foreign PostToolUseFailure hook dropped")
	}
	// Notification now has the user's hook + ours = 2 groups.
	notif := asSlice(hooks["Notification"])
	if len(notif) != 2 {
		t.Fatalf("Notification groups = %d, want 2 (user + twm)", len(notif))
	}
	var sawUser, sawTwm bool
	for _, g := range notif {
		cmd := commandOf(t, g)
		if strings.Contains(cmd, "say hi") {
			sawUser = true
		}
		if strings.Contains(cmd, twmHookMarker) {
			sawTwm = true
		}
	}
	if !sawUser || !sawTwm {
		t.Fatalf("expected both user and twm Notification hooks, sawUser=%v sawTwm=%v", sawUser, sawTwm)
	}

	// Re-merging must not duplicate our entry (still 2 groups).
	out2 := mergeClaudeHooks(out, bin)
	if n := len(asSlice(asMap(out2["hooks"])["Notification"])); n != 2 {
		t.Fatalf("re-merge duplicated twm hook: %d groups", n)
	}
}

func TestInstallClaudeHooksWritesAndDryRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	os.WriteFile(path, []byte(`{"theme":"dark"}`), 0o644)

	// dry-run: nothing written.
	var buf bytes.Buffer
	if err := installClaudeHooks(&buf, path, bin, true); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "hook") {
		t.Fatalf("dry-run wrote hooks: %s", data)
	}
	if !strings.Contains(buf.String(), "Would update") {
		t.Fatalf("dry-run output: %q", buf.String())
	}

	// real run: file gains hooks, theme preserved.
	buf.Reset()
	if err := installClaudeHooks(&buf, path, bin, false); err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	data, _ = os.ReadFile(path)
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("output not valid json: %v\n%s", err, data)
	}
	if got["theme"] != "dark" {
		t.Errorf("theme not preserved")
	}
	if len(asSlice(asMap(got["hooks"])["Stop"])) != 1 {
		t.Errorf("Stop hook not installed")
	}

	// second real run: idempotent, reports up-to-date, file unchanged.
	before, _ := os.ReadFile(path)
	buf.Reset()
	if err := installClaudeHooks(&buf, path, bin, false); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatalf("second run changed the file")
	}
	if !strings.Contains(buf.String(), "already up to date") {
		t.Fatalf("expected up-to-date message, got %q", buf.String())
	}
}

func TestCodexInstructions(t *testing.T) {
	s := codexInstructions(bin)
	if !strings.Contains(s, "notify = [\""+bin+"\"") {
		t.Fatalf("codex notify line wrong: %q", s)
	}
	if !strings.Contains(s, "--codex") {
		t.Fatalf("codex snippet missing --codex flag")
	}
}

// commandOf extracts the single hook command string from a group.
func commandOf(t *testing.T, group any) string {
	t.Helper()
	hs := asSlice(asMap(group)["hooks"])
	if len(hs) == 0 {
		t.Fatal("group has no hooks")
	}
	cmd, _ := asMap(hs[0])["command"].(string)
	return cmd
}
