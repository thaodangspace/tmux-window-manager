package attachlink

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
)

type fakeTmux struct {
	mu         sync.Mutex
	paneExists bool
	paneErr    error
	clients    []tmuxcli.Client
	listErr    error
	switchErr  error
	switches   []struct{ client, pane string }
}

func (f *fakeTmux) PaneExists(string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.paneExists, f.paneErr
}
func (f *fakeTmux) ListClients() ([]tmuxcli.Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]tmuxcli.Client(nil), f.clients...), f.listErr
}
func (f *fakeTmux) SwitchClient(client, pane string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.switches = append(f.switches, struct{ client, pane string }{client, pane})
	return f.switchErr
}
func (f *fakeTmux) setClients(clients ...tmuxcli.Client) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clients = clients
}
func (f *fakeTmux) switchCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.switches)
}

func startTestServer(t *testing.T, fake *fakeTmux, lifetime time.Duration) *Server {
	t.Helper()
	s, err := Listen(context.Background(), "%20", fake, lifetime)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	t.Cleanup(func() {
		s.Close()
		s.Wait()
	})
	return s
}

func request(t *testing.T, method, url string) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request error = %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	return resp, string(body)
}

func TestListenRejectsInvalidConfiguration(t *testing.T) {
	fake := &fakeTmux{}
	if _, err := Listen(context.Background(), "session:window", fake, time.Minute); !errors.Is(err, ErrInvalidTarget) {
		t.Fatalf("invalid target error = %v", err)
	}
	if _, err := Listen(context.Background(), "%20", nil, time.Minute); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil tmux error = %v", err)
	}
	if _, err := Listen(context.Background(), "%20", fake, 0); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid lifetime error = %v", err)
	}
}

func TestListenUsesOpaqueLoopbackURLAndRandomToken(t *testing.T) {
	first := startTestServer(t, &fakeTmux{}, time.Minute)
	second := startTestServer(t, &fakeTmux{}, time.Minute)
	for _, s := range []*Server{first, second} {
		if !strings.HasPrefix(s.URL(), "http://127.0.0.1:") {
			t.Fatalf("URL = %q, want IPv4 loopback", s.URL())
		}
		if strings.Contains(s.URL(), "%20") || len(s.token) != 22 || !isURLSafeToken(s.token) {
			t.Fatalf("URL/token is not opaque and URL-safe: URL=%q token=%q", s.URL(), s.token)
		}
	}
	if first.token == second.token {
		t.Fatal("two attach listeners reused a token")
	}
}

func TestGETIsInertAndReturnsSecureAutoPostPage(t *testing.T) {
	fake := &fakeTmux{paneExists: true, clients: []tmuxcli.Client{{Name: "tty", Activity: 1}}}
	s := startTestServer(t, fake, time.Minute)
	resp, body := request(t, http.MethodGet, s.URL())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET status = %d, want 200", resp.StatusCode)
	}
	if fake.switchCount() != 0 {
		t.Fatal("GET switched tmux")
	}
	for _, header := range []string{"Cache-Control", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy", "Content-Security-Policy"} {
		if resp.Header.Get(header) == "" {
			t.Errorf("GET missing security header %q", header)
		}
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("weak security headers: nosniff=%q frame=%q", resp.Header.Get("X-Content-Type-Options"), resp.Header.Get("X-Frame-Options"))
	}
	for _, want := range []string{"method=\"post\"", "Attach in tmux", "<script nonce=\"", "document.forms[0].submit()"} {
		if !strings.Contains(body, want) {
			t.Errorf("GET body missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "%20") {
		t.Fatal("GET body disclosed pane target")
	}
}

func TestInvalidRoutesAndMethodsAreGeneric(t *testing.T) {
	s := startTestServer(t, &fakeTmux{}, time.Minute)
	badHost, err := http.NewRequest(http.MethodGet, s.URL(), nil)
	if err != nil {
		t.Fatal(err)
	}
	badHost.Host = "127.0.0.1:1"
	resp, err := http.DefaultClient.Do(badHost)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("wrong Host status = %d, want 404", resp.StatusCode)
	}
	_ = resp.Body.Close()

	for _, tt := range []struct {
		name   string
		method string
		url    string
		status int
	}{
		{name: "wrong token", method: http.MethodGet, url: strings.TrimSuffix(s.URL(), s.token) + "wrong", status: http.StatusNotFound},
		{name: "wrong path", method: http.MethodGet, url: strings.TrimSuffix(s.URL(), s.route) + "/other", status: http.StatusNotFound},
		{name: "query", method: http.MethodGet, url: s.URL() + "?probe=1", status: http.StatusNotFound},
		{name: "wrong method", method: http.MethodPut, url: s.URL(), status: http.StatusMethodNotAllowed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := request(t, tt.method, tt.url)
			if resp.StatusCode != tt.status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.status)
			}
			if strings.Contains(body, s.token) || strings.Contains(body, "%20") {
				t.Fatalf("response disclosed capability data: %q", body)
			}
			if tt.name == "wrong method" && resp.Header.Get("Allow") != "GET, POST" {
				t.Fatalf("Allow = %q", resp.Header.Get("Allow"))
			}
		})
	}
}

func TestPOSTSwitchesMostRecentClientAndConsumesLink(t *testing.T) {
	fake := &fakeTmux{
		paneExists: true,
		clients: []tmuxcli.Client{
			{Name: "older", Activity: 10},
			{Name: "newer", Activity: 20},
		},
	}
	s := startTestServer(t, fake, time.Minute)
	resp, body := request(t, http.MethodPost, s.URL())
	if resp.StatusCode != http.StatusOK || body != "Attached. Return to Ghostty." {
		t.Fatalf("POST = %d/%q", resp.StatusCode, body)
	}
	if fake.switchCount() != 1 || fake.switches[0].client != "newer" || fake.switches[0].pane != "%20" {
		t.Fatalf("switches = %#v", fake.switches)
	}
	s.Wait()
}

func TestPOSTDeadTargetStopsListener(t *testing.T) {
	fake := &fakeTmux{paneExists: false}
	s := startTestServer(t, fake, time.Minute)
	resp, body := request(t, http.MethodPost, s.URL())
	if resp.StatusCode != http.StatusGone || !strings.Contains(body, "no longer available") {
		t.Fatalf("POST = %d/%q", resp.StatusCode, body)
	}
	s.Wait()
	if fake.switchCount() != 0 {
		t.Fatal("dead target attempted a switch")
	}
}

func TestPOSTRetryableFailuresRemainAvailable(t *testing.T) {
	t.Run("no client", func(t *testing.T) {
		fake := &fakeTmux{paneExists: true}
		s := startTestServer(t, fake, time.Minute)
		resp, _ := request(t, http.MethodPost, s.URL())
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("status = %d, want 409", resp.StatusCode)
		}
		fake.setClients(tmuxcli.Client{Name: "tty", Activity: 1})
		resp, _ = request(t, http.MethodPost, s.URL())
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("retry status = %d, want 200", resp.StatusCode)
		}
	})

	t.Run("tmux error", func(t *testing.T) {
		fake := &fakeTmux{paneExists: true, clients: []tmuxcli.Client{{Name: "tty", Activity: 1}}, switchErr: errors.New("failed")}
		s := startTestServer(t, fake, time.Minute)
		resp, body := request(t, http.MethodPost, s.URL())
		if resp.StatusCode != http.StatusServiceUnavailable || body != "tmux is temporarily unavailable" {
			t.Fatalf("status/body = %d/%q", resp.StatusCode, body)
		}
	})
}

func TestPOSTIsSingleSuccessUnderConcurrency(t *testing.T) {
	fake := &fakeTmux{paneExists: true, clients: []tmuxcli.Client{{Name: "tty", Activity: 1}}}
	s := startTestServer(t, fake, time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = http.Post(s.URL(), "", nil)
		}()
	}
	wg.Wait()
	s.Wait()
	if got := fake.switchCount(); got != 1 {
		t.Fatalf("switch count = %d, want 1", got)
	}
}

func TestContextCancellationAndExpiryStopServer(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	fake := &fakeTmux{}
	s, err := Listen(ctx, "%20", fake, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	waitDone(t, s)

	s = startTestServer(t, fake, 20*time.Millisecond)
	waitDone(t, s)
}

func waitDone(t *testing.T, s *Server) {
	t.Helper()
	done := make(chan struct{})
	go func() { s.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("server did not stop by deadline")
	}
}
