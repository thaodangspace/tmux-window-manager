package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// twmHookMarker identifies a hook command older releases installed. It matches
// the binary basename, independent of the absolute install path.
const twmHookMarker = "tmux-window-manager"

// newUninstallHooksCommand removes the Claude Code hooks and points at the
// Codex notify line that older releases installed. Agent status no longer
// needs them: the watcher reads the panes directly.
func newUninstallHooksCommand() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "uninstall-hooks",
		Short: "Remove the agent hooks older releases installed",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			if err := uninstallClaudeHooks(out, claudeSettingsPath(), dryRun); err != nil {
				return err
			}
			reportCodexNotify(out, codexConfigPath())
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing")
	return cmd
}

func claudeSettingsPath() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, "settings.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "settings.json")
}

func codexConfigPath() string {
	if dir := os.Getenv("CODEX_HOME"); dir != "" {
		return filepath.Join(dir, "config.toml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "config.toml")
}

// uninstallClaudeHooks drops every twm hook group from the Claude settings
// file, preserving everything else, and writes it back atomically.
func uninstallClaudeHooks(out io.Writer, path string, dryRun bool) error {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		fmt.Fprintf(out, "No Claude settings at %s\n", path)
		return nil
	}
	if err != nil {
		return err
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	updated, events := removeClaudeHooks(settings)
	if len(events) == 0 {
		fmt.Fprintf(out, "No twm hooks in %s\n", path)
		return nil
	}
	if dryRun {
		fmt.Fprintf(out, "Would remove twm hooks from %s: %s\n", path, strings.Join(events, ", "))
		return nil
	}
	rendered, err := marshalSettings(updated)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	if err := atomicWrite(path, rendered, mode); err != nil {
		return err
	}
	fmt.Fprintf(out, "Removed twm hooks from %s: %s\n", path, strings.Join(events, ", "))
	return nil
}

// removeClaudeHooks returns settings without twm hook groups, dropping events
// (and the hooks key) left empty, plus the sorted events it touched. Pure.
func removeClaudeHooks(settings map[string]any) (map[string]any, []string) {
	hooks := asMap(settings["hooks"])
	var events []string
	for event, v := range hooks {
		groups := asSlice(v)
		kept := groups[:0:0]
		for _, g := range groups {
			if !groupIsTwm(g) {
				kept = append(kept, g)
			}
		}
		if len(kept) == len(groups) {
			continue
		}
		events = append(events, event)
		if len(kept) == 0 {
			delete(hooks, event)
		} else {
			hooks[event] = kept
		}
	}
	sort.Strings(events)
	if len(events) > 0 && len(hooks) == 0 {
		delete(settings, "hooks")
	}
	return settings, events
}

// groupIsTwm reports whether a hook group contains a twm-owned command.
func groupIsTwm(group any) bool {
	for _, h := range asSlice(asMap(group)["hooks"]) {
		if cmd, _ := asMap(h)["command"].(string); strings.Contains(cmd, twmHookMarker) && strings.Contains(cmd, " hook") {
			return true
		}
	}
	return false
}

// reportCodexNotify points at a twm `notify = [...]` line in the Codex config.
// The file is left alone: TOML comments and layout would not survive a rewrite.
func reportCodexNotify(out io.Writer, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "notify") && strings.Contains(trimmed, twmHookMarker) {
			fmt.Fprintf(out, "Remove the twm notify line from %s (line %d):\n  %s\n", path, i+1, trimmed)
		}
	}
}

func asMap(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func asSlice(v any) []any {
	if s, ok := v.([]any); ok {
		return s
	}
	return nil
}

// marshalSettings renders settings as stable, indented JSON (map keys sorted by
// encoding/json), with a trailing newline.
func marshalSettings(settings map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(settings); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".settings_*.json")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	_ = os.Chmod(name, mode)
	return os.Rename(name, path)
}
