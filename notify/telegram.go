// Package notify provides optional outbound notifications for agent lifecycle
// events. It deliberately has no dependency on the agent payload or status
// store packages so transports remain isolated from hook normalization and
// persistence.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	botTokenEnv = "TWM_TELEGRAM_BOT_TOKEN"
	chatIDEnv   = "TWM_TELEGRAM_CHAT_ID"

	officialTelegramBaseURL = "https://api.telegram.org"
	telegramTimeout         = 2 * time.Second
	maxTelegramTextChars    = 4000 // safely below Telegram's 4096-character limit
	maxTelegramResponse     = 64 * 1024
)

// Kind is a user-attention transition that can be rendered as a notification.
type Kind string

const (
	Waiting   Kind = "waiting"
	Completed Kind = "completed"
)

// Event is the vendor-neutral input used to compose a notification. Detail is
// used only for Waiting; Completed deliberately omits assistant response text.
type Event struct {
	Kind      Kind
	Agent     string
	Cwd       string
	SessionID string
	Prompt    string
	Detail    string
	// AttachURL is an optional validated loopback URL that focuses the
	// originating tmux pane. Invalid URLs are ignored by Compose.
	AttachURL string
}

// Config contains the credentials for one Telegram destination. Callers should
// not print this value because BotToken is a secret.
type Config struct {
	BotToken string
	ChatID   string
}

// ErrPartialConfig means exactly one required Telegram variable is set.
var ErrPartialConfig = errors.New("telegram configuration is incomplete")

// ConfigFromEnv returns cfg and enabled=true when both Telegram variables are
// present. Neither variable means disabled. A partial configuration is invalid
// but never includes either configured value in its error.
func ConfigFromEnv() (cfg Config, enabled bool, err error) {
	return configFromLookup(os.Getenv)
}

func configFromLookup(lookup func(string) string) (cfg Config, enabled bool, err error) {
	return validateConfig(lookup(botTokenEnv), lookup(chatIDEnv))
}

func validateConfig(token, chat string) (cfg Config, enabled bool, err error) {
	cfg = Config{
		BotToken: strings.TrimSpace(token),
		ChatID:   strings.TrimSpace(chat),
	}
	hasToken := cfg.BotToken != ""
	hasChat := cfg.ChatID != ""
	switch {
	case !hasToken && !hasChat:
		return Config{}, false, nil
	case !hasToken || !hasChat:
		return Config{}, false, ErrPartialConfig
	default:
		return cfg, true, nil
	}
}

// Compose renders a Telegram MarkdownV2 message. The first line remains an
// unformatted summary; labels on the remaining lines are bold. Unknown kinds
// are not actionable and return an empty string. All fields are normalized,
// escaped, and truncated without splitting UTF-8.
func Compose(event Event) string {
	agent := displayAgent(event.Agent)
	project := projectName(event.Cwd)

	var icon, action string
	switch event.Kind {
	case Waiting:
		icon, action = "🔔", "needs input"
	case Completed:
		icon, action = "✅", "finished"
	default:
		return ""
	}

	sessionID := sanitizeText(event.SessionID)
	if sessionID == "" {
		sessionID = "unknown-session"
	}
	prompt := sanitizeText(event.Prompt)
	if prompt == "" {
		prompt = "unavailable"
	}

	message := icon + " " + agent + " " + action + " · " + project +
		"\nSession: " + sessionID +
		"\nPrompt: " + prompt
	if event.Kind == Waiting {
		if detail := sanitizeText(event.Detail); detail != "" {
			message += "\nDetail: " + detail
		}
	}

	if link, ok := validatedAttachLink(event.AttachURL); ok {
		// Reserve space for the complete link before formatting MarkdownV2.
		// This ensures truncation can never produce a broken action.
		message = truncateWithSuffix(message, "\n"+link, maxTelegramTextChars)
	}
	return formatMarkdownV2(truncateRunes(message, maxTelegramTextChars))
}

func formatMarkdownV2(message string) string {
	lines := strings.Split(message, "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "[Attach in tmux](http://127.0.0.1:") {
			// validatedAttachLink has already restricted the URL to a safe
			// opaque loopback route. Keep Markdown link syntax intact.
			continue
		}
		if i == 0 {
			lines[i] = escapeMarkdownV2(line)
			continue
		}
		label, value, ok := strings.Cut(line, ": ")
		if !ok {
			lines[i] = escapeMarkdownV2(line)
			continue
		}
		lines[i] = "*" + escapeMarkdownV2(label+":") + "* " + escapeMarkdownV2(value)
	}
	return strings.Join(lines, "\n")
}

func truncateWithSuffix(prefix, suffix string, max int) string {
	if utf8.RuneCountInString(prefix)+utf8.RuneCountInString(suffix) <= max {
		return prefix + suffix
	}
	budget := max - utf8.RuneCountInString(suffix)
	if budget <= 0 {
		return truncateRunes(suffix, max)
	}
	return truncateRunes(prefix, budget) + suffix
}

func validatedAttachLink(raw string) (string, bool) {
	if strings.TrimSpace(raw) == "" {
		return "", false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() == "" {
		return "", false
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return "", false
	}
	const prefix = "/attach/"
	if !strings.HasPrefix(u.Path, prefix) {
		return "", false
	}
	token := strings.TrimPrefix(u.Path, prefix)
	if len(token) < 22 || strings.Contains(token, "/") || !isURLSafeToken(token) {
		return "", false
	}
	return "[Attach in tmux](" + raw + ")", true
}

func isURLSafeToken(token string) bool {
	for _, r := range token {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' || r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

func escapeMarkdownV2(s string) string {
	const special = `\_*[]()~` + "`" + `>#+-=|{}.!`
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if strings.ContainsRune(special, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func displayAgent(agent string) string {
	agent = sanitizeText(agent)
	if agent == "" {
		return "Agent"
	}
	runes := []rune(agent)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

func projectName(cwd string) string {
	cwd = strings.TrimSpace(cwd)
	if cwd == "" {
		return "unknown-project"
	}
	name := filepath.Base(filepath.Clean(cwd))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return "unknown-project"
	}
	name = sanitizeText(name)
	if name == "" {
		return "unknown-project"
	}
	return name
}

// sanitizeText collapses whitespace and strips control/format characters so a
// vendor payload cannot alter terminal/debug formatting or Telegram rendering.
func sanitizeText(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r), unicode.IsSpace(r):
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	runes := []rune(s)
	return string(runes[:max-1]) + "…"
}

// Delivery error categories are exported for errors.Is checks. Their messages
// intentionally contain no URL, response body, chat ID, or token.
var (
	ErrRequest   = errors.New("telegram request could not be created")
	ErrTransport = errors.New("telegram transport failed")
	ErrHTTP      = errors.New("telegram returned an unsuccessful HTTP status")
	ErrResponse  = errors.New("telegram returned an invalid response")
	ErrRejected  = errors.New("telegram rejected the message")
)

// Telegram sends MarkdownV2 messages to one configured Telegram destination.
type Telegram struct {
	endpoint string
	chatID   string
	client   *http.Client
}

// NewTelegram creates the production sender. The token is retained only inside
// the endpoint and must never be included in returned errors.
func NewTelegram(cfg Config) *Telegram {
	return newTelegram(cfg, officialTelegramBaseURL, &http.Client{Timeout: telegramTimeout})
}

// newTelegram is the deterministic test seam for endpoint and HTTP behavior.
// Production callers always use NewTelegram and the official HTTPS endpoint.
func newTelegram(cfg Config, baseURL string, client *http.Client) *Telegram {
	if client == nil {
		client = &http.Client{Timeout: telegramTimeout}
	}
	cloned := *client
	cloned.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return errors.New("telegram redirects are not allowed")
	}
	return &Telegram{
		endpoint: strings.TrimRight(baseURL, "/") + "/bot" + url.PathEscape(cfg.BotToken) + "/sendMessage",
		chatID:   cfg.ChatID,
		client:   &cloned,
	}
}

// Send calls Telegram Bot API sendMessage. All returned errors are deliberately
// sanitized because standard net/http errors can include the token-bearing URL.
func (t *Telegram) Send(ctx context.Context, text string) error {
	if t == nil || t.client == nil || t.endpoint == "" || t.chatID == "" || text == "" {
		return ErrRequest
	}
	payload := struct {
		ChatID             string `json:"chat_id"`
		Text               string `json:"text"`
		ParseMode          string `json:"parse_mode"`
		LinkPreviewOptions struct {
			IsDisabled bool `json:"is_disabled"`
		} `json:"link_preview_options"`
	}{
		ChatID: t.chatID, Text: text, ParseMode: "MarkdownV2",
		LinkPreviewOptions: struct {
			IsDisabled bool `json:"is_disabled"`
		}{IsDisabled: true},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return ErrRequest
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.endpoint, bytes.NewReader(body))
	if err != nil {
		return ErrRequest
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.client.Do(req)
	if err != nil {
		return ErrTransport
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, maxTelegramResponse+1))
	if err != nil {
		return ErrResponse
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("%w: status %d", ErrHTTP, resp.StatusCode)
	}
	if len(responseBody) > maxTelegramResponse {
		return ErrResponse
	}
	var result struct {
		OK bool `json:"ok"`
	}
	if json.Unmarshal(responseBody, &result) != nil {
		return ErrResponse
	}
	if !result.OK {
		return ErrRejected
	}
	return nil
}
