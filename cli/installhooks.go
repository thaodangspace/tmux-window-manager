package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

// claudeHookEvents are the lifecycle events twm wires into Claude Code. Each maps
// to a `twm hook <Event>` command; the handler turns it into a status write.
var claudeHookEvents = []string{
	"SessionStart",
	"UserPromptSubmit",
	"Notification",
	"Stop",
	"SessionEnd",
}

// twmHookMarker identifies a hook command this tool owns, so reinstalling
// replaces our entries instead of duplicating them. It matches the binary
// basename, independent of the absolute install path.
const twmHookMarker = "tmux-window-manager"

func newInstallHooksCommand() *cobra.Command {
	var (
		claude bool
		codex  bool
		dryRun bool
	)
	cmd := &cobra.Command{
		Use:   "install-hooks",
		Short: "Wire agent status hooks into Claude Code and Codex",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !claude && !codex { // default: do both
				claude, codex = true, true
			}
			bin, err := os.Executable()
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if claude {
				if err := installClaudeHooks(out, claudeSettingsPath(), bin, dryRun); err != nil {
					return err
				}
			}
			if codex {
				fmt.Fprint(out, codexInstructions(bin))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&claude, "claude", false, "install Claude Code hooks (default: both)")
	cmd.Flags().BoolVar(&codex, "codex", false, "print the Codex config snippet (default: both)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change without writing")
	return cmd
}

func claudeSettingsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "settings.json")
}

// installClaudeHooks merges twm hook entries into the Claude settings file,
// preserving every other setting and any non-twm hooks, then writes it back
// atomically. Idempotent: a second run produces a byte-identical file. With
// dryRun it reports the intended change and writes nothing.
func installClaudeHooks(out io.Writer, path, bin string, dryRun bool) error {
	if path == "" {
		return fmt.Errorf("could not resolve Claude settings path")
	}

	var settings map[string]any
	mode := os.FileMode(0o644)
	switch data, err := os.ReadFile(path); {
	case err == nil:
		if fi, e := os.Stat(path); e == nil {
			mode = fi.Mode().Perm()
		}
		if err := json.Unmarshal(data, &settings); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	case os.IsNotExist(err):
		settings = map[string]any{}
	default:
		return err
	}
	if settings == nil {
		settings = map[string]any{}
	}

	updated := mergeClaudeHooks(settings, bin)
	rendered, err := marshalSettings(updated)
	if err != nil {
		return err
	}

	current, _ := os.ReadFile(path)
	if bytes.Equal(bytes.TrimRight(current, "\n"), bytes.TrimRight(rendered, "\n")) {
		fmt.Fprintf(out, "Claude hooks already up to date: %s\n", path)
		return nil
	}
	if dryRun {
		fmt.Fprintf(out, "Would update %s with twm hooks for: %s\n", path, strings.Join(claudeHookEvents, ", "))
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if err := atomicWrite(path, rendered, mode); err != nil {
		return err
	}
	fmt.Fprintf(out, "Installed twm hooks into %s (%s)\n", path, strings.Join(claudeHookEvents, ", "))
	return nil
}

// mergeClaudeHooks returns settings with our hook entries present for every
// event in claudeHookEvents. For each event it drops any previously-installed
// twm group and appends exactly one fresh group, leaving the user's own hooks
// untouched. Pure (no I/O) so it is unit-testable.
func mergeClaudeHooks(settings map[string]any, bin string) map[string]any {
	hooks := asMap(settings["hooks"])
	for _, event := range claudeHookEvents {
		groups := asSlice(hooks[event])
		kept := groups[:0:0]
		for _, g := range groups {
			if !groupIsTwm(g) {
				kept = append(kept, g)
			}
		}
		kept = append(kept, map[string]any{
			"hooks": []any{
				map[string]any{"type": "command", "command": hookCommand(bin, event)},
			},
		})
		hooks[event] = kept
	}
	settings["hooks"] = hooks
	return settings
}

// hookCommand builds the shell command Claude runs for an event.
func hookCommand(bin, event string) string {
	c := shellQuote(bin) + " hook"
	if event != "" {
		c += " " + event
	}
	return c
}

// groupIsTwm reports whether a hook group contains a twm-owned command.
func groupIsTwm(group any) bool {
	g := asMap(group)
	for _, h := range asSlice(g["hooks"]) {
		hm := asMap(h)
		if cmd, _ := hm["command"].(string); strings.Contains(cmd, twmHookMarker) && strings.Contains(cmd, " hook") {
			return true
		}
	}
	return false
}

func codexInstructions(bin string) string {
	var b strings.Builder
	b.WriteString("\n# Codex — add to ~/.codex/config.toml:\n\n")
	fmt.Fprintf(&b, "notify = [%s, \"hook\", \"--agent\", \"codex\", \"--codex\"]\n", strconv.Quote(bin))
	b.WriteString("\n# Codex passes its agent-turn-complete payload as a JSON arg; twm reads it\n")
	b.WriteString("# from stdin/argv. As Codex's hook events stabilize you can also add matching\n")
	b.WriteString("# [hooks] entries pointing at:  " + shellQuote(bin) + " hook --agent codex <Event>\n")
	return b.String()
}

// --- small helpers -------------------------------------------------------

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
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
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

// shellQuote wraps s in single quotes when it contains characters a shell would
// interpret, escaping embedded single quotes. Mirrors picker.ShellQuote but kept
// local so install-hooks has no dependency on the picker package.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	safe := true
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' ||
			r == '/' || r == '.' || r == '_' || r == '-') {
			safe = false
			break
		}
	}
	if safe {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
