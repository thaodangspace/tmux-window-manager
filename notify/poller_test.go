package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadPollerConfig(t *testing.T) {
	defaults := PollerConfig{
		Endpoint: defaultPollerEndpoint, Model: defaultPollerModel, Scan: defaultPollerScan,
		Settle: defaultPollerSettle, Interval: 2 * time.Minute, Lines: defaultPollerLines, Timeout: defaultPollerTimeout,
	}
	tests := []struct {
		name     string
		contents string
		want     PollerConfig
		enabled  bool
	}{
		{name: "absent section keeps timings, judge off", contents: "[telegram]\n", want: defaults},
		{name: "defaults", contents: "[poller]\nenabled = true\n", enabled: true, want: defaults},
		{name: "overrides", contents: "[poller]\nenabled = true\nendpoint = \"http://pi:1234/v1/\"\nmodel = \"m\"\ninterval_seconds = 60\nscan_seconds = 2\nsettle_seconds = 0\nlines = 30\ntimeout_seconds = 20\n", enabled: true, want: PollerConfig{
			Endpoint: "http://pi:1234/v1", Model: "m", Scan: 2 * time.Second, Interval: time.Minute, Lines: 30, Timeout: 20 * time.Second,
		}},
		{name: "out of range values keep defaults", contents: "[poller]\nenabled = true\ninterval_seconds = 1\nscan_seconds = 0\nsettle_seconds = -1\n", enabled: true, want: defaults},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), configFileName)
			if err := os.WriteFile(path, []byte(tt.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			got, enabled, err := loadPollerConfig(path)
			if err != nil || enabled != tt.enabled || got != tt.want {
				t.Fatalf("loadPollerConfig() = %+v, %v, %v; want %+v, %v", got, enabled, err, tt.want, tt.enabled)
			}
		})
	}
}

func completion(content string) string {
	b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": content}}}})
	return string(b)
}

func TestJudge(t *testing.T) {
	var req struct {
		Model          string `json:"model"`
		Messages       []struct{ Role, Content string }
		ResponseFormat struct {
			Type string `json:"type"`
		} `json:"response_format"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		_, _ = io.WriteString(w, completion(`{"state":"waiting","summary":"needs\napproval","model":"Opus 4.5"}`))
	}))
	defer srv.Close()

	cfg := PollerConfig{Endpoint: srv.URL + "/v1", Model: "google/gemma-4-12b", Timeout: 5 * time.Second}
	v, err := Judge(context.Background(), cfg, "claude", strings.Repeat("x", maxJudgeScreenChars+10)+"TAIL")
	if err != nil {
		t.Fatal(err)
	}
	if v.State != ScreenWaiting || !v.Notify() || v.Summary != "needs approval" || v.Model != "Opus 4.5" {
		t.Fatalf("verdict = %+v", v)
	}
	if req.Model != "google/gemma-4-12b" || req.ResponseFormat.Type != "json_schema" || len(req.Messages) != 2 {
		t.Fatalf("unexpected request: %+v", req)
	}
	if user := req.Messages[1].Content; !strings.Contains(user, "Agent: claude") || !strings.Contains(user, "TAIL") ||
		len(user) > maxJudgeScreenChars+100 {
		t.Fatalf("screen not bounded to its tail: %d chars", len(user))
	}
}

func TestJudgeFailures(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "http error", status: http.StatusInternalServerError, body: "boom"},
		{name: "not a completion", status: http.StatusOK, body: "{}"},
		{name: "free text verdict", status: http.StatusOK, body: completion("it is waiting")},
		{name: "unknown state", status: http.StatusOK, body: completion(`{"state":"sleeping","summary":""}`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = io.WriteString(w, tt.body)
			}))
			defer srv.Close()
			_, err := Judge(context.Background(), PollerConfig{Endpoint: srv.URL, Timeout: time.Second}, "claude", "screen")
			if !errors.Is(err, ErrJudge) {
				t.Fatalf("err = %v, want ErrJudge", err)
			}
		})
	}
}

func TestVerdictNotify(t *testing.T) {
	for state, want := range map[string]bool{
		ScreenWorking: false, ScreenIdle: false, ScreenWaiting: true, ScreenCompleted: true, ScreenError: true,
	} {
		if got := (Verdict{State: state}).Notify(); got != want {
			t.Errorf("Notify(%s) = %v, want %v", state, got, want)
		}
	}
}
