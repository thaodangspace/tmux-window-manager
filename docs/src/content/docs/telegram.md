---
title: Telegram notifications
description: Configure optional notifications when an agent needs input or finishes.
---

Telegram delivery is an optional, best-effort side effect of the
[pane watcher](/notification-poller/). It needs the model judge
(`[poller] enabled = true`): TWM sends a message when the model judges an
agent's settled screen as waiting for input, failed (sent as "needs input"), or
finished. Each settled screen notifies at most once, and screens already on
display when the watcher starts never notify. It works for every
[supported agent](/agent-status/#supported-agents).

## Configure credentials

Create `~/.config/twm.toml`:

```toml
[telegram]
bot_token = "<bot-token>"
chat_id = "<chat-id>"
```

Protect the token:

```sh
chmod 600 ~/.config/twm.toml
```

When `XDG_CONFIG_HOME` is set, the file is `$XDG_CONFIG_HOME/twm.toml`.
Non-empty environment variables override file values:

```sh
export TWM_TELEGRAM_BOT_TOKEN='<bot-token>'
export TWM_TELEGRAM_CHAT_ID='<chat-id>'
```

Both values are required. With neither configured, notifications are silently
disabled. A partial configuration is rejected and logged; the watcher keeps
running. The file is re-read for every notification, so edits apply without a
restart.

## Filtering and content options

Optional keys in the same `[telegram]` section tune what is sent. The defaults
are shown:

```toml
[telegram]
# Add the model's one-sentence summary of the finished turn to "finished"
# messages. Off by default because it describes the agent's work and leaves
# your machine through Telegram's servers.
include_response = false
# Skip "finished" messages when the agent was busy (first screen change to
# last change) for less than this; 0 sends every turn.
min_turn_seconds = 30
# Skip messages while the agent's pane is on screen in a tmux client that had
# keyboard activity in the last two minutes.
skip_when_focused = true
```

These options are read from the file even when the credentials come from the
environment.

## Message and delivery behavior

A message looks like this:

```text
✅ Claude finished · tmux-window-manager · 4m12s
Where: work:3
Model: Opus 4.5
Response: Added the Ctrl-X binding and refused killing the last session.
```

It includes the agent name, project-directory basename, busy duration, the tmux
`session:window` of the agent pane, and the model name the judge read from the
screen. "Needs input" messages add the model's summary as `Detail` (prefixed
with `⚠️` for errors); "finished" messages add it as `Response` only with
`include_response = true`. Long fields are shortened. Full paths, screen
content, PIDs, and credentials are not sent.

"Needs input" messages alert normally; "finished" messages are delivered
silently so they appear in the chat without a sound.

Delivery uses Telegram's Bot API over HTTPS with a two-second timeout and no
retry. Telegram failures never affect status. Set `TWM_DEBUG=1` in the tmux
server environment and restart the watcher for redacted diagnostics in
`$TMPDIR/twm_debug.log`.

## Disable notifications

Remove the `[telegram]` section and unset the environment overrides:

```sh
unset TWM_TELEGRAM_BOT_TOKEN TWM_TELEGRAM_CHAT_ID
```
