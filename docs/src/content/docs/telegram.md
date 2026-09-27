---
title: Telegram notifications
description: Configure optional lifecycle notifications when an agent needs input or finishes.
---

Telegram delivery is an optional, best-effort side effect of a successful status
write. TWM sends messages only for `Notification` (the agent needs user input)
and `Stop` (the agent finished a turn). Other lifecycle events never send a
message. The integration works for any agent that invokes `twm hook` with one of
those event names; Codex's `notify` payload is not treated as a `Stop` event.

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
disabled. A partial configuration is rejected without failing the agent hook.

## Filtering and content options

Optional keys in the same `[telegram]` section tune what is sent. The defaults
are shown:

```toml
[telegram]
# Add an excerpt (up to 800 characters) of the agent's last reply to
# "finished" messages. Off by default because the reply text leaves your
# machine through Telegram's servers.
include_response = false
# Skip "finished" messages for turns shorter than this; 0 sends every turn.
min_turn_seconds = 30
# Skip messages while the agent's pane is on screen in a tmux client that had
# keyboard activity in the last two minutes.
skip_when_focused = true
```

These options are read from the file even when the credentials come from the
environment.

Claude's "waiting for your input" reminder (sent about a minute after a turn
ends) repeats the preceding "finished" message, so it is delivered only when
nothing was sent for that turn — for example, when a short turn was skipped and
you have since walked away. Permission prompts are always delivered unless the
pane is focused.

## Message and delivery behavior

A message looks like this:

```text
✅ Claude finished · tmux-window-manager · 4m12s
Where: work:3
Model: Opus
Prompt: add Ctrl-X to kill the selected session
Response: Added the binding and refused killing the last session…
```

It includes the agent name, project-directory basename, turn duration, the tmux
`session:window` of the agent pane (a shortened session ID when the agent is not
running in tmux), the model, and the prompt of the current turn. `Notification`
messages add the supplied detail; `Stop` messages add the reply excerpt only
with `include_response = true`. Long fields are shortened. Full paths,
transcript content, PIDs, and credentials are not sent.

"Needs input" messages alert normally; "finished" messages are delivered
silently so they appear in the chat without a sound.

Delivery uses Telegram's Bot API over HTTPS with a two-second timeout and no
retry or resident daemon. It happens only after the status DB write succeeds.
Telegram failures never roll back status or fail the agent hook. Set
`TWM_HOOK_DEBUG=1` before starting the agent for redacted diagnostics in
`$TMPDIR/twm_hook.log`.

## Disable notifications

Remove the `[telegram]` section and unset the environment overrides:

```sh
unset TWM_TELEGRAM_BOT_TOKEN TWM_TELEGRAM_CHAT_ID
```
