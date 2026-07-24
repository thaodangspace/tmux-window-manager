package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

func TestConfigFromLookup(t *testing.T) {
	const (
		token = "123456:super-secret"
		chat  = "-100123456"
	)
	tests := []struct {
		name    string
		values  map[string]string
		enabled bool
		wantErr error
	}{
		{name: "disabled", values: map[string]string{}},
		{name: "token only", values: map[string]string{botTokenEnv: token}, wantErr: ErrPartialConfig},
		{name: "chat only", values: map[string]string{chatIDEnv: chat}, wantErr: ErrPartialConfig},
		{name: "whitespace only disabled", values: map[string]string{botTokenEnv: " \t", chatIDEnv: "\n"}},
		{name: "enabled", values: map[string]string{botTokenEnv: "  " + token + " ", chatIDEnv: " " + chat + " "}, enabled: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, enabled, err := configFromLookup(func(key string) string { return tt.values[key] })
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if enabled != tt.enabled {
				t.Fatalf("enabled = %v, want %v", enabled, tt.enabled)
			}
			if tt.enabled {
				if cfg.BotToken != token || cfg.ChatID != chat {
					t.Fatalf("config not trimmed: %+v", cfg)
				}
			} else if cfg != (Config{}) {
				t.Fatalf("disabled/error config must be empty: %+v", cfg)
			}
			if err != nil && (strings.Contains(err.Error(), token) || strings.Contains(err.Error(), chat)) {
				t.Fatalf("configuration error leaked a credential: %v", err)
			}
		})
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv(botTokenEnv, "token")
	t.Setenv(chatIDEnv, "chat")
	cfg, enabled, err := ConfigFromEnv()
	if err != nil || !enabled || cfg.BotToken != "token" || cfg.ChatID != "chat" {
		t.Fatalf("ConfigFromEnv() = %+v, %v, %v", cfg, enabled, err)
	}
}

func TestCompose(t *testing.T) {
	tests := []struct {
		name  string
		event Event
		want  string
	}{
		{
			name: "waiting",
			event: Event{Kind: Waiting, Agent: "claude", Cwd: "/Users/dev/code/tmux-window-manager",
				SessionID: "session-1", Prompt: " Check\nstatus ", Detail: " Permission\nrequired\tto continue. "},
			want: "🔔 Claude needs input · tmux\\-window\\-manager\n*Session:* session\\-1\n*Prompt:* Check status\n*Detail:* Permission required to continue\\.",
		},
		{
			name: "completed omits assistant detail",
			event: Event{Kind: Completed, Agent: "claude", Cwd: "/Users/dev/code/project/",
				SessionID: "session-2", Prompt: "Run tests", Detail: "Tests pass."},
			want: "✅ Claude finished · project\n*Session:* session\\-2\n*Prompt:* Run tests",
		},
		{
			name:  "missing session and prompt",
			event: Event{Kind: Completed, Agent: "claude", Cwd: "/work/project"},
			want:  "✅ Claude finished · project\n*Session:* unknown\\-session\n*Prompt:* unavailable",
		},
		{
			name:  "unknown agent and project",
			event: Event{Kind: Waiting, Cwd: "/"},
			want:  "🔔 Agent needs input · unknown\\-project\n*Session:* unknown\\-session\n*Prompt:* unavailable",
		},
		{
			name: "control and format characters",
			event: Event{Kind: Waiting, Agent: "cl\x00aude", Cwd: "/work/my\u200bproject",
				SessionID: "s\u200b1", Prompt: "do\r\nthe\tthing", Detail: "one\r\ntwo\u200dthree"},
			want: "🔔 Cl aude needs input · my project\n*Session:* s 1\n*Prompt:* do the thing\n*Detail:* one two three",
		},
		{
			name: "escapes dynamic MarkdownV2 syntax",
			event: Event{Kind: Waiting, Agent: "pi_agent", Cwd: "/work/my.project",
				SessionID: "s_[1]", Prompt: "fix *all* (now)!"},
			want: "🔔 Pi\\_agent needs input · my\\.project\n*Session:* s\\_\\[1\\]\n*Prompt:* fix \\*all\\* \\(now\\)\\!",
		},
		{
			name:  "unknown kind",
			event: Event{Kind: Kind("other"), Agent: "claude", Cwd: "/work/project", Detail: "secret"},
			want:  "",
		},
		{
			name:  "valid attach link",
			event: Event{Kind: Completed, Agent: "claude", Cwd: "/work/project", SessionID: "session-3", Prompt: "Run tests", AttachURL: "http://127.0.0.1:49152/attach/Abc_123-opaque-token-xx"},
			want:  "✅ Claude finished · project\n*Session:* session\\-3\n*Prompt:* Run tests\n[Attach in tmux](http://127.0.0.1:49152/attach/Abc_123-opaque-token-xx)",
		},
		{
			name:  "invalid attach link omitted",
			event: Event{Kind: Completed, Agent: "claude", Cwd: "/work/project", AttachURL: "http://localhost:49152/attach/secret"},
			want:  "✅ Claude finished · project\n*Session:* unknown\\-session\n*Prompt:* unavailable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Compose(tt.event); got != tt.want {
				t.Fatalf("Compose() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestComposeAttachLinkPreservedWhenTextIsTruncated(t *testing.T) {
	const link = "http://127.0.0.1:49152/attach/opaque-token-1234567890"
	got := Compose(Event{
		Kind:      Waiting,
		Agent:     "claude",
		Cwd:       "/private/work/project",
		SessionID: "session-id",
		Prompt:    strings.Repeat("界", maxTelegramTextChars+100),
		AttachURL: link,
	})
	if !strings.Contains(got, "[Attach in tmux]("+link+")") {
		t.Fatalf("attach link was not preserved: %q", got[len(got)-100:])
	}
	if !utf8.ValidString(got) {
		t.Fatal("message is not valid UTF-8")
	}
	if strings.Contains(got, "/private/work/project") {
		t.Fatal("message contains an absolute path")
	}
}

func TestComposeRejectsUnsafeAttachLinks(t *testing.T) {
	for _, link := range []string{
		"https://127.0.0.1:49152/attach/token",
		"http://localhost:49152/attach/token",
		"http://127.0.0.1/attach/token",
		"http://127.0.0.1:49152/other/token",
		"http://127.0.0.1:49152/attach/token/extra",
		"http://127.0.0.1:49152/attach/token?x=1",
		"http://127.0.0.1:49152/attach/token)",
	} {
		t.Run(link, func(t *testing.T) {
			got := Compose(Event{Kind: Completed, Agent: "claude", Cwd: "/work/project", AttachURL: link})
			if strings.Contains(got, "Attach in tmux") || strings.Contains(got, link) {
				t.Fatalf("unsafe link was rendered: %q", got)
			}
		})
	}
}

func TestComposeBoundsMultibyteText(t *testing.T) {
	got := Compose(Event{
		Kind:      Completed,
		Agent:     "claude",
		Cwd:       "/private/work/project",
		SessionID: "session-id",
		Prompt:    strings.Repeat("界", maxTelegramTextChars+100),
	})
	if !utf8.ValidString(got) {
		t.Fatal("message is not valid UTF-8")
	}
	rendered := strings.NewReplacer("*", "", "\\", "").Replace(got)
	if n := utf8.RuneCountInString(rendered); n != maxTelegramTextChars {
		t.Fatalf("rendered message length = %d runes, want %d", n, maxTelegramTextChars)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("truncated message has no marker: %q", got[len(got)-8:])
	}
	for _, forbidden := range []string{"/private/work/project", "bot-token"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("message contains forbidden value %q", forbidden)
		}
	}
}

func TestTelegramSendSuccess(t *testing.T) {
	const (
		token = "123456:secret-token"
		chat  = "-100123"
		text  = "Claude finished · project"
	)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != "/bot"+token+"/sendMessage" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q", got)
		}
		var payload struct {
			ChatID             string `json:"chat_id"`
			Text               string `json:"text"`
			ParseMode          string `json:"parse_mode"`
			LinkPreviewOptions struct {
				IsDisabled bool `json:"is_disabled"`
			} `json:"link_preview_options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if payload.ChatID != chat || payload.Text != text {
			t.Errorf("payload = %+v", payload)
		}
		if payload.ParseMode != "MarkdownV2" {
			t.Errorf("parse_mode = %q, want MarkdownV2", payload.ParseMode)
		}
		if !payload.LinkPreviewOptions.IsDisabled {
			t.Error("link previews are enabled")
		}
		io.WriteString(w, `{"ok":true,"result":{"message_id":1}}`)
	}))
	defer server.Close()

	sender := newTelegram(Config{BotToken: token, ChatID: chat}, server.URL, server.Client())
	if err := sender.Send(context.Background(), text); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls = %d, want 1", calls.Load())
	}
}

func TestTelegramSendFailuresAreCategorizedAndRedacted(t *testing.T) {
	const token = "never-leak-this-token"
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr error
	}{
		{
			name: "non 2xx",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTooManyRequests)
				io.WriteString(w, `{"ok":false,"description":"token `+token+`"}`)
			},
			wantErr: ErrHTTP,
		},
		{
			name:    "malformed response",
			handler: func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, `{`) },
			wantErr: ErrResponse,
		},
		{
			name: "api rejected",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				io.WriteString(w, `{"ok":false,"description":"bad `+token+`"}`)
			},
			wantErr: ErrRejected,
		},
		{
			name: "oversized response",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				io.WriteString(w, strings.Repeat("x", maxTelegramResponse+1))
			},
			wantErr: ErrResponse,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()
			sender := newTelegram(Config{BotToken: token, ChatID: "chat"}, server.URL, server.Client())
			err := sender.Send(context.Background(), "message")
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want category %v", err, tt.wantErr)
			}
			assertNoSecret(t, err, token)
		})
	}
}

func TestTelegramTransportFailureAndTimeoutAreRedacted(t *testing.T) {
	const token = "transport-secret"
	t.Run("transport", func(t *testing.T) {
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, fmt.Errorf("network failed near %s", token)
		})}
		sender := newTelegram(Config{BotToken: token, ChatID: "chat"}, "https://example.invalid", client)
		err := sender.Send(context.Background(), "message")
		if !errors.Is(err, ErrTransport) {
			t.Fatalf("error = %v, want %v", err, ErrTransport)
		}
		assertNoSecret(t, err, token)
	})

	t.Run("timeout", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			time.Sleep(100 * time.Millisecond)
			io.WriteString(w, `{"ok":true}`)
		}))
		defer server.Close()
		client := server.Client()
		client.Timeout = 10 * time.Millisecond
		sender := newTelegram(Config{BotToken: token, ChatID: "chat"}, server.URL, client)
		started := time.Now()
		err := sender.Send(context.Background(), "message")
		if !errors.Is(err, ErrTransport) {
			t.Fatalf("error = %v, want %v", err, ErrTransport)
		}
		if elapsed := time.Since(started); elapsed > 80*time.Millisecond {
			t.Fatalf("timeout was not bounded: %v", elapsed)
		}
		assertNoSecret(t, err, token)
	})
}

func TestTelegramRejectsRedirect(t *testing.T) {
	const token = "redirect-secret"
	var redirected atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		redirected.Store(true)
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()

	sender := newTelegram(Config{BotToken: token, ChatID: "chat"}, source.URL, source.Client())
	err := sender.Send(context.Background(), "message")
	if !errors.Is(err, ErrTransport) {
		t.Fatalf("error = %v, want %v", err, ErrTransport)
	}
	if redirected.Load() {
		t.Fatal("client followed redirect")
	}
	assertNoSecret(t, err, token)
}

func TestTelegramRequestAndProductionDefaults(t *testing.T) {
	const token = "request-secret"
	bad := newTelegram(Config{BotToken: token, ChatID: "chat"}, "://bad-url", &http.Client{})
	err := bad.Send(context.Background(), "message")
	if !errors.Is(err, ErrRequest) {
		t.Fatalf("error = %v, want %v", err, ErrRequest)
	}
	assertNoSecret(t, err, token)

	production := NewTelegram(Config{BotToken: token, ChatID: "chat"})
	if !strings.HasPrefix(production.endpoint, "https://api.telegram.org/") {
		t.Fatalf("production endpoint = %q", production.endpoint)
	}
	if production.client.Timeout != telegramTimeout {
		t.Fatalf("production timeout = %v, want %v", production.client.Timeout, telegramTimeout)
	}

	for name, sender := range map[string]*Telegram{
		"nil sender": nil,
		"empty chat": newTelegram(Config{BotToken: token}, "https://example.invalid", &http.Client{}),
	} {
		t.Run(name, func(t *testing.T) {
			if err := sender.Send(context.Background(), "message"); !errors.Is(err, ErrRequest) {
				t.Fatalf("error = %v, want %v", err, ErrRequest)
			}
		})
	}
	if err := production.Send(context.Background(), ""); !errors.Is(err, ErrRequest) {
		t.Fatalf("empty message error = %v, want %v", err, ErrRequest)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func assertNoSecret(t *testing.T, err error, secret string) {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked secret: %v", err)
	}
}
