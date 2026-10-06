package notify

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

const (
	alerterBin          = "alerter"
	defaultMacOSSound   = "default"
	defaultMacOSTimeout = 12 * 60 * 60 // seconds a notification waits for a click

	maxMacOSTitleChars = 120
	maxMacOSBodyChars  = 400
)

// ErrNotifierMissing means alerter is not on PATH.
var ErrNotifierMissing = errors.New("alerter not found")

// ErrNotifierFailed means the notification could not be posted.
var ErrNotifierFailed = errors.New("macos notification failed")

// MacOSConfig configures Notification Center delivery via alerter.
type MacOSConfig struct {
	// TerminalBundleID is the app brought to the front when the notification
	// is clicked (e.g. com.mitchellh.ghostty). Empty means no activation.
	TerminalBundleID string
	// Sound is played for "needs input" notifications; empty is silent.
	// Completed turns are always silent, matching Telegram.
	Sound string
	// TimeoutSeconds closes an unclicked notification (and its waiting
	// process) after this long. Zero waits until it is clicked or dismissed.
	TimeoutSeconds int
	Options        Options
}

// LoadMacOSConfig reads the [macos] section of the twm config file. The
// backend is enabled only on darwin with `enabled = true`. When
// terminal_bundle_id is unset, the bundle id macOS exports to processes started
// by a terminal app ($__CFBundleIdentifier) is used.
func LoadMacOSConfig() (cfg MacOSConfig, enabled bool, err error) {
	if runtime.GOOS != "darwin" {
		return MacOSConfig{}, false, nil
	}
	path, err := configPath()
	if err != nil {
		return MacOSConfig{}, false, err
	}
	return loadMacOSConfig(path, os.Getenv)
}

func loadMacOSConfig(path string, lookup func(string) string) (MacOSConfig, bool, error) {
	file, err := readFileConfig(path)
	if err != nil {
		return MacOSConfig{}, false, err
	}
	m := file.MacOS
	if !m.Enabled {
		return MacOSConfig{}, false, nil
	}
	cfg := MacOSConfig{
		TerminalBundleID: strings.TrimSpace(m.TerminalBundleID),
		Sound:            defaultMacOSSound,
		TimeoutSeconds:   defaultMacOSTimeout,
		Options:          m.options(),
	}
	if cfg.TerminalBundleID == "" {
		cfg.TerminalBundleID = strings.TrimSpace(lookup("__CFBundleIdentifier"))
	}
	if m.Sound != nil {
		cfg.Sound = strings.TrimSpace(*m.Sound)
	}
	if m.TimeoutSeconds != nil && *m.TimeoutSeconds >= 0 {
		cfg.TimeoutSeconds = *m.TimeoutSeconds
	}
	return cfg, true, nil
}

// Plain is a notification rendered for a plain-text surface such as the macOS
// Notification Center.
type Plain struct {
	Title    string
	Subtitle string
	Body     string
}

// ComposePlain renders event without markup. Unknown kinds return ok=false.
func ComposePlain(event Event) (Plain, bool) {
	agent := displayAgent(event.Agent)
	var p Plain
	switch event.Kind {
	case Waiting:
		p.Title = "🔔 " + agent + " needs input"
	case Completed:
		p.Title = "✅ " + agent + " finished"
	default:
		return Plain{}, false
	}

	parts := []string{projectName(event.Cwd)}
	if location := sanitizeText(event.Location); location != "" {
		parts = append(parts, location)
	}
	if d := formatDuration(event.Duration); d != "" {
		parts = append(parts, d)
	}
	p.Subtitle = truncateRunes(strings.Join(parts, " · "), maxMacOSTitleChars)

	var lines []string
	switch event.Kind {
	case Waiting:
		if detail := truncateRunes(sanitizeText(event.Detail), maxDetailChars); detail != "" {
			lines = append(lines, detail)
		}
	case Completed:
		if response := truncateRunes(sanitizeText(event.Response), maxResponseChars); response != "" {
			lines = append(lines, "Response: "+response)
		}
	}
	if prompt := truncateRunes(sanitizeText(event.Prompt), maxPromptChars); prompt != "" {
		lines = append(lines, "Prompt: "+prompt)
	}
	if len(lines) == 0 {
		lines = append(lines, "Open the pane for details") // alerter needs a message
	}
	p.Body = truncateRunes(strings.Join(lines, "\n"), maxMacOSBodyChars)
	return p, true
}

// MacOSGroup is the notification group of an agent session: a newer event for
// the same session replaces the older notification. It is filename-safe.
func MacOSGroup(event Event) string {
	var b strings.Builder
	for _, r := range "twm-" + event.Agent + "-" + event.SessionID {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

// AlerterArgv returns the full alerter command line for event. alerter blocks
// until the notification is clicked, dismissed, or times out, then prints a
// JSON result (see Clicked). Values use the --opt=value form so text starting
// with "-" is never parsed as an option.
func AlerterArgv(cfg MacOSConfig, event Event) ([]string, error) {
	p, ok := ComposePlain(event)
	if !ok {
		return nil, ErrRequest
	}
	bin, err := exec.LookPath(alerterBin)
	if err != nil {
		return nil, ErrNotifierMissing
	}
	argv := []string{
		bin,
		"--title=" + p.Title,
		"--subtitle=" + p.Subtitle,
		"--message=" + p.Body,
		"--group=" + MacOSGroup(event),
		"--timeout=" + strconv.Itoa(cfg.TimeoutSeconds),
		"--json",
	}
	if event.Kind == Waiting && cfg.Sound != "" {
		argv = append(argv, "--sound="+cfg.Sound)
	}
	return argv, nil
}

// Clicked reports whether alerter's JSON output describes a click on the
// notification body or one of its actions.
func Clicked(output []byte) bool {
	var result struct {
		ActivationType string `json:"activationType"`
	}
	if json.Unmarshal(output, &result) != nil {
		return false
	}
	switch result.ActivationType {
	case "contentsClicked", "actionClicked":
		return true
	}
	return false
}
