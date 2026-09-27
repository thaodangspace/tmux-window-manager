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

## Message and delivery behavior

Messages include the agent name, project-directory basename, session ID, first
user prompt, and—for `Notification`—the supplied detail. Full paths, transcript
content, PIDs, model names, assistant responses, and credentials are not sent.

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
