//go:build darwin || linux

package attachlink

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/thaodangspace/tmux-window-manager/tmuxcli"
)

var startupTimeout = 250 * time.Millisecond

var ErrStartup = errors.New("attach helper did not become ready")

// Handle owns a detached helper after it has reported readiness. Commit keeps
// the helper alive; Cancel terminates and reaps it. Both operations are safe to
// call repeatedly.
type Handle struct {
	url string

	mu       sync.Mutex
	settled  bool
	cmd      *exec.Cmd
	readPipe io.ReadCloser
}

// Launch starts the current executable in hidden serve-attach mode and waits
// for a validated loopback URL. The pane target is transferred over stdin, not
// placed in argv or the generated URL.
func Launch(ctx context.Context, executable, target string) (*Handle, error) {
	if !tmuxcli.ValidPaneID(target) || strings.TrimSpace(executable) == "" {
		return nil, ErrStartup
	}
	if ctx == nil {
		ctx = context.Background()
	}

	cmd := exec.Command(executable, "serve-attach")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, ErrStartup
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, ErrStartup
	}
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, ErrStartup
	}

	if _, err := io.WriteString(stdin, target+"\n"); err != nil {
		_ = stdin.Close()
		return nil, failCommand(cmd, stdout)
	}
	_ = stdin.Close()

	ready := make(chan string, 1)
	go readReady(stdout, ready)
	timer := time.NewTimer(startupTimeout)
	defer timer.Stop()
	select {
	case raw := <-ready:
		readyURL, err := validateReadyURL(raw)
		if err != nil {
			return nil, failCommand(cmd, stdout)
		}
		return &Handle{url: readyURL, cmd: cmd, readPipe: stdout}, nil
	case <-timer.C:
		return nil, failCommand(cmd, stdout)
	case <-ctx.Done():
		return nil, failCommand(cmd, stdout)
	}
}

func readReady(stdout io.Reader, ready chan<- string) {
	reader := bufio.NewReader(io.LimitReader(stdout, 2048))
	line, err := reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		ready <- ""
		return
	}
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "READY ") {
		ready <- strings.TrimSpace(strings.TrimPrefix(line, "READY "))
		return
	}
	ready <- ""
}

func validateReadyURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" ||
		u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() == "" {
		return "", ErrStartup
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return "", ErrStartup
	}
	if _, ok := requestToken(u.Path); !ok {
		return "", ErrStartup
	}
	if !strings.HasPrefix(u.Path, attachPrefix) || len(strings.TrimPrefix(u.Path, attachPrefix)) < 22 {
		return "", ErrStartup
	}
	return raw, nil
}

func failCommand(cmd *exec.Cmd, stdout io.ReadCloser) error {
	_ = stdout.Close()
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	wait := make(chan error, 1)
	go func() { wait <- cmd.Wait() }()
	select {
	case <-wait:
	case <-time.After(time.Second):
	}
	return ErrStartup
}

// URL returns the validated loopback capability URL.
func (h *Handle) URL() string {
	if h == nil {
		return ""
	}
	return h.url
}

// Commit releases the parent-side process handle while leaving the helper
// alive under its own hard lifetime.
func (h *Handle) Commit() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.settled {
		return nil
	}
	h.settled = true
	_ = h.readPipe.Close()
	return h.cmd.Process.Release()
}

// Cancel terminates and reaps the helper. It is best effort and never exposes
// process details to callers.
func (h *Handle) Cancel() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.settled {
		return nil
	}
	h.settled = true
	_ = h.readPipe.Close()
	if h.cmd.Process == nil {
		return nil
	}
	if err := h.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("cancel attach helper: %w", err)
	}
	_ = h.cmd.Wait()
	return nil
}
