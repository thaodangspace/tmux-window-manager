package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/thaodangspace/tmux-window-manager/notify"
)

type notifier interface {
	Notify(ctx context.Context, event notify.Event) error
}

// backend is one configured delivery channel with its own filters. err
// reports a configuration problem; the backend is skipped.
type backend struct {
	name   string
	sender notifier
	opts   notify.Options
	err    error
}

type backendFactory func() []backend

// notifiersFromConfig returns every enabled backend (Telegram, macOS). It is
// re-read on every judgment so config edits apply without a restart.
func notifiersFromConfig() []backend {
	var backends []backend
	if cfg, enabled, err := notify.LoadConfig(); err != nil {
		backends = append(backends, backend{name: "telegram", err: err})
	} else if enabled {
		backends = append(backends, backend{name: "telegram", sender: notify.NewTelegram(cfg), opts: cfg.Options})
	}
	if cfg, enabled, err := notify.LoadMacOSConfig(); err != nil {
		backends = append(backends, backend{name: "macos", err: err})
	} else if enabled {
		backends = append(backends, backend{name: "macos", sender: newMacOSSender(cfg), opts: cfg.Options})
	}
	return backends
}

// focusRecent is how recently a tmux client must have had input for a visible
// agent pane to count as watched.
const focusRecent = 2 * time.Minute

// deliver fans event out to every backend, applying each backend's filters.
// skip, when set, returns why a backend should not receive the event ("" =
// send). response is attached to completed events for backends that opted in
// with include_response. It reports whether any backend delivered.
func deliver(backends []backend, base notify.Event, watched bool, response string, skip func(notify.Options) string) (sent bool) {
	defer func() {
		if recover() != nil {
			debugf("notify: delivery failed (panic)")
		}
	}()
	for _, b := range backends {
		if b.err != nil {
			debugf("notify: %s configuration failed (%s)", b.name, notifyErrorCategory(b.err))
			continue
		}
		if b.sender == nil {
			continue
		}
		if skip != nil {
			if reason := skip(b.opts); reason != "" {
				debugf("notify: %s skipped (%s)", b.name, reason)
				continue
			}
		}
		if b.opts.SkipWhenFocused && watched {
			debugf("notify: %s skipped (pane is focused)", b.name)
			continue
		}
		event := base
		if event.Kind == notify.Completed && b.opts.IncludeResponse {
			event.Response = response
		}
		if err := b.sender.Notify(context.Background(), event); err != nil {
			debugf("notify: %s delivery failed (%s)", b.name, notifyErrorCategory(err))
			continue
		}
		sent = true
	}
	return sent
}

// notifyErrorCategory deliberately drops the original error text because it
// may contain a token-bearing URL or another secret.
func notifyErrorCategory(err error) string {
	switch {
	case errors.Is(err, notify.ErrPartialConfig):
		return "partial-config"
	case errors.Is(err, notify.ErrConfigFile):
		return "config-file"
	case errors.Is(err, notify.ErrRequest):
		return "request"
	case errors.Is(err, notify.ErrTransport):
		return "transport"
	case errors.Is(err, notify.ErrHTTP):
		return "http"
	case errors.Is(err, notify.ErrResponse):
		return "response"
	case errors.Is(err, notify.ErrRejected):
		return "rejected"
	case errors.Is(err, notify.ErrNotifierMissing):
		return "alerter-missing"
	case errors.Is(err, notify.ErrNotifierFailed):
		return "macos-failed"
	case errors.Is(err, notify.ErrJudge):
		return "judge"
	default:
		return "unknown"
	}
}

// debugf appends a line to $TMPDIR/twm_debug.log when TWM_DEBUG is set.
// Silent otherwise.
func debugf(format string, a ...any) {
	if os.Getenv("TWM_DEBUG") == "" {
		return
	}
	f, err := os.OpenFile(os.TempDir()+"/twm_debug.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, time.Now().Format("15:04:05 ")+format+"\n", a...)
}
