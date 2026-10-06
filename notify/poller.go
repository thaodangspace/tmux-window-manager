package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	defaultPollerEndpoint = "http://localhost:1234/v1"
	defaultPollerModel    = "google/gemma-4-12b"
	defaultPollerInterval = 2 * time.Minute
	defaultPollerScan     = 5 * time.Second
	defaultPollerSettle   = 6 * time.Second
	defaultPollerLines    = 60
	// A cold LM Studio model load takes well over a minute; warm calls take
	// a few seconds.
	defaultPollerTimeout = 3 * time.Minute

	maxJudgeScreenChars = 8000
	maxJudgeResponse    = 64 * 1024
	maxSummaryChars     = 200
)

// ErrJudge means the local model could not be reached or returned an
// unusable verdict.
var ErrJudge = errors.New("judge request failed")

type pollerKeys struct {
	Enabled         bool   `toml:"enabled"`
	Endpoint        string `toml:"endpoint"`
	Model           string `toml:"model"`
	IntervalSeconds *int   `toml:"interval_seconds"`
	ScanSeconds     *int   `toml:"scan_seconds"`
	SettleSeconds   *int   `toml:"settle_seconds"`
	Lines           *int   `toml:"lines"`
	TimeoutSeconds  *int   `toml:"timeout_seconds"`
}

// PollerConfig configures the pane watcher. It captures every agent pane each
// Scan; a screen that stopped changing for Settle is sent to a local
// OpenAI-compatible model (LM Studio) that classifies it and decides whether
// to notify.
type PollerConfig struct {
	Endpoint string // base URL ending in /v1
	Model    string
	Scan     time.Duration
	Settle   time.Duration
	// Interval re-judges a screen the model last called working that has not
	// changed since (a silent long-running tool).
	Interval time.Duration
	Lines    int // trailing screen lines sent to the model
	Timeout  time.Duration
}

// LoadPollerConfig reads the [poller] section. The returned config always
// carries usable scan timings; enabled (`enabled = true`) turns on the model
// judge and therefore notifications.
func LoadPollerConfig() (cfg PollerConfig, enabled bool, err error) {
	path, err := configPath()
	if err == nil {
		cfg, enabled, err = loadPollerConfig(path)
	}
	if err != nil {
		cfg, _, _ = loadPollerConfig("") // defaults; the judge stays off
		return cfg, false, err
	}
	return cfg, enabled, nil
}

func loadPollerConfig(path string) (PollerConfig, bool, error) {
	file, err := readFileConfig(path)
	if err != nil {
		return PollerConfig{}, false, err
	}
	p := file.Poller
	cfg := PollerConfig{
		Endpoint: strings.TrimRight(strings.TrimSpace(p.Endpoint), "/"),
		Model:    strings.TrimSpace(p.Model),
		Scan:     defaultPollerScan,
		Settle:   defaultPollerSettle,
		Interval: defaultPollerInterval,
		Lines:    defaultPollerLines,
		Timeout:  defaultPollerTimeout,
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = defaultPollerEndpoint
	}
	if cfg.Model == "" {
		cfg.Model = defaultPollerModel
	}
	if p.IntervalSeconds != nil && *p.IntervalSeconds >= 10 {
		cfg.Interval = time.Duration(*p.IntervalSeconds) * time.Second
	}
	if p.ScanSeconds != nil && *p.ScanSeconds >= 1 {
		cfg.Scan = time.Duration(*p.ScanSeconds) * time.Second
	}
	if p.SettleSeconds != nil && *p.SettleSeconds >= 0 {
		cfg.Settle = time.Duration(*p.SettleSeconds) * time.Second
	}
	if p.Lines != nil && *p.Lines > 0 {
		cfg.Lines = *p.Lines
	}
	if p.TimeoutSeconds != nil && *p.TimeoutSeconds > 0 {
		cfg.Timeout = time.Duration(*p.TimeoutSeconds) * time.Second
	}
	return cfg, p.Enabled, nil
}

// Screen states the judge can report.
const (
	ScreenWorking   = "working"
	ScreenWaiting   = "waiting"
	ScreenCompleted = "completed"
	ScreenError     = "error"
	ScreenIdle      = "idle"
)

// Verdict is the judge's reading of one agent screen.
type Verdict struct {
	State   string `json:"state"`
	Summary string `json:"summary"`
	Model   string `json:"model"` // model name printed on screen, if any
}

// Notify reports whether the state needs the user's attention.
func (v Verdict) Notify() bool {
	return v.State == ScreenWaiting || v.State == ScreenCompleted || v.State == ScreenError
}

const judgeSystemPrompt = `You watch the terminal screen of an AI coding agent CLI (Claude Code, Codex, Gemini CLI, OpenCode, Aider, pi or similar) running in tmux and decide whether its human should be notified.

Classify the bottom of the screen as exactly one state:
- working: the agent is still running: a spinner or progress line, "esc to interrupt", thinking, tools executing, output still streaming.
- waiting: the agent is blocked on the human: a permission or approval prompt, a yes/no or numbered choice menu, a direct question to the user, a plan awaiting approval.
- completed: the agent finished its turn: a final answer or summary sits above an empty input prompt and nothing is running.
- error: the agent stopped on a failure it cannot recover from by itself (API error, rate limit, crash, repeated failing command).
- idle: nothing new: a fresh session, an empty prompt with no new result, or a plain shell.

summary: one short sentence (at most 20 words) for the notification, in the language the conversation uses, saying what the agent needs or what it finished. Never include secrets, tokens or credentials.

model: the AI model name exactly as printed in the agent's header or status line (for example "Opus 4.5" or "gpt-5-codex"), or "" when no model name is visible. Never guess.`

var judgeSchema = json.RawMessage(`{
  "type": "object",
  "properties": {
    "state": {"type": "string", "enum": ["working", "waiting", "completed", "error", "idle"]},
    "summary": {"type": "string"},
    "model": {"type": "string"}
  },
  "required": ["state", "summary", "model"]
}`)

// judgeClient is a test seam; redirects are refused so the screen text never
// leaves the configured endpoint.
var judgeClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// Judge sends the trailing screen of an agent pane to the configured model
// and returns its verdict.
func Judge(ctx context.Context, cfg PollerConfig, agent, screen string) (Verdict, error) {
	if len(screen) > maxJudgeScreenChars {
		screen = screen[len(screen)-maxJudgeScreenChars:]
	}
	body, err := json.Marshal(map[string]any{
		"model":       cfg.Model,
		"temperature": 0,
		"messages": []map[string]string{
			{"role": "system", "content": judgeSystemPrompt},
			{"role": "user", "content": fmt.Sprintf("Agent: %s\nScreen:\n```\n%s\n```", agent, screen)},
		},
		"response_format": map[string]any{
			"type":        "json_schema",
			"json_schema": map[string]any{"name": "verdict", "strict": true, "schema": judgeSchema},
		},
	})
	if err != nil {
		return Verdict{}, ErrJudge
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.Endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Verdict{}, ErrJudge
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := judgeClient.Do(req)
	if err != nil {
		return Verdict{}, fmt.Errorf("%w: transport", ErrJudge)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxJudgeResponse))
	if resp.StatusCode != http.StatusOK {
		return Verdict{}, fmt.Errorf("%w: http %d", ErrJudge, resp.StatusCode)
	}
	return parseJudgeResponse(data)
}

func parseJudgeResponse(data []byte) (Verdict, error) {
	var completion struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(data, &completion) != nil || len(completion.Choices) == 0 {
		return Verdict{}, fmt.Errorf("%w: response", ErrJudge)
	}
	var v Verdict
	content := strings.TrimSpace(completion.Choices[0].Message.Content)
	if json.Unmarshal([]byte(content), &v) != nil {
		return Verdict{}, fmt.Errorf("%w: verdict", ErrJudge)
	}
	switch v.State {
	case ScreenWorking, ScreenWaiting, ScreenCompleted, ScreenError, ScreenIdle:
	default:
		return Verdict{}, fmt.Errorf("%w: state", ErrJudge)
	}
	v.Summary = truncateRunes(sanitizeText(v.Summary), maxSummaryChars)
	v.Model = truncateRunes(sanitizeText(v.Model), 40)
	return v, nil
}
