---
title: Pane watcher and model judge
description: How the watcher reads agent panes, and how a local LM Studio model decides status and notifications.
---

The pane watcher is the only source of agent status and notifications. The
plugin starts it on every tmux load (`watch --detach --replace`), so it also
restarts after each rebuild, and it exits when the tmux server goes away.

On its own the watcher only tells working from settled: a changing screen is
`working`, a settled screen is `idle`, and nothing is sent. Enabling the model
judge lets a local model read each settled screen and decide whether the agent
is waiting on you, finished, or failed. That verdict drives both the picker
badge and the [Telegram](/telegram/) and [macOS](/macos-notifications/)
notifications.

## Setup

1. Run LM Studio's server (or another OpenAI-compatible endpoint) with a model
   loaded. The default is `google/gemma-4-12b` at `http://localhost:1234/v1`.
2. Configure at least one notification backend if you want notifications.
3. Enable the judge in `~/.config/twm.toml`:

   ```toml
   [poller]
   enabled = true
   ```

4. Restart the watcher: reload tmux (`tmux source ~/.tmux.conf`), or run
   `tmux-window-manager watch --detach --replace`.

Check the model's reading of every agent pane without writing or sending
anything:

```bash
tmux-window-manager watch --once
```

It prints one line per agent pane: pane ID, `session:window`, agent, state,
model, and summary. `--once` requires `enabled = true`.

## Configuration

All keys are optional; defaults are shown.

```toml
[poller]
enabled = false
endpoint = "http://localhost:1234/v1"
model = "google/gemma-4-12b"
scan_seconds = 5        # capture agent panes this often; minimum 1
settle_seconds = 6      # unchanged this long before the screen is judged; 0 or more
interval_seconds = 120  # re-judge a screen still judged "working"; minimum 10
lines = 60              # trailing screen lines captured and sent to the model
timeout_seconds = 180   # per model call; the first call may wait for the model to load
```

`[poller]` is read when the watcher starts, so restart it after editing.
Backend sections (`[telegram]`, `[macos]`) are re-read for every notification.

## How it decides

- **Every scan**, the watcher captures each pane running a coding agent and
  hashes the screen with digits removed, so timers and token counters do not
  count as change. A changed screen sets the pane to `working`.
- **A screen unchanged for `settle_seconds`** is sent to the model once, as a
  `chat/completions` request with a JSON schema response: a state (`working`,
  `waiting`, `completed`, `error`, `idle`), a one-sentence summary, and the AI
  model name if it is printed on screen.
- **The verdict becomes the status row.** `completed` and `idle` are shown as
  idle; the summary becomes the row's detail, and the model name becomes the
  picker's model label.
- **Long silent tools.** A screen judged `working` that stays unchanged is
  re-judged every `interval_seconds`.
- **Failures.** If the model call fails, the pane falls back to idle and is
  retried after 30 seconds.

The screen text goes only to the configured endpoint. Redirects are refused.

## Notifications

- **What notifies.** `waiting`, `completed`, and `error` verdicts. Errors are
  sent as "needs input".
- **Once per screen.** A settled screen notifies at most once; the pane must
  change and settle again before it can notify again.
- **Starting is silent.** Screens already present when the watcher starts never
  notify.
- **Filters.** Backends with `skip_when_focused = true` skip panes you are
  looking at. `min_turn_seconds` skips "finished" notifications whose busy
  time (first screen change to last change) was shorter.
- **Content.** The model's summary is the body of every notification: the
  detail of "needs input" messages and the summary of "finished" messages.
  Set `include_response = false` on a backend to leave it out of "finished"
  messages.

## Operating it

| Command | Effect |
| --- | --- |
| `watch --once` | Print each agent pane's verdict; no writes, no delivery |
| `watch --detach --replace` | Restart the background watcher (applies `[poller]` edits) |

The watcher holds a lock on `watch.pid` next to the status database, so only one
runs at a time. Set `TWM_DEBUG=1` in the tmux server environment and restart the
watcher to log verdicts and delivery failures to `$TMPDIR/twm_debug.log`.
