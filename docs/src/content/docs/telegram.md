---
title: Telegram notifications
description: Configure optional Claude lifecycle notifications and understand their security boundary.
---

Telegram is an optional side effect of successful Claude status writes. It sends
best-effort notifications for `Notification` and `Stop` events only. Codex and
Pi events do not send Telegram messages.

## Configure credentials

Create `~/.config/twm.toml`:

```toml
[telegram]
bot_token = "<bot-token>"
chat_id = "<chat-id>"
```

Restrict access because this file contains the bot token:

```sh
chmod 600 ~/.config/twm.toml
```

When `$XDG_CONFIG_HOME` is set, the configuration path is
`$XDG_CONFIG_HOME/twm.toml`.

Non-empty environment variables override the corresponding file values:

```sh
export TWM_TELEGRAM_BOT_TOKEN='<bot-token>'
export TWM_TELEGRAM_CHAT_ID='<chat-id>'
claude
```

Environment values must be set before Claude starts because hooks inherit its
environment. The TOML file is read for every eligible hook, so file edits do not
require a restart. Both credentials are required; a partial configuration is
skipped as an error.

## Message contents

Every message includes a status icon, the project directory basename, session
ID, and first user prompt. A notification also includes its detail. A stop
message deliberately excludes assistant response text.

The plugin does **not** send full paths, transcript files, other transcript
content, PIDs, model names, or credentials. Delivery uses Telegram's Bot API
over HTTPS with a two-second timeout and no retry, outbox, or background daemon.
A delivery failure does not roll back agent status or fail the hook.

## Attach links

When an eligible Claude notification has a live `TMUX_PANE`, the hook can start a
short-lived local helper and add an **Attach in tmux** link. This feature is
tertiary best effort:

- The helper binds only to `127.0.0.1`, keeps GET requests inert, and requires an exact token-gated POST.
- A 128-bit in-memory token is single-use and expires after 15 minutes.
- Success switches the most recently active existing tmux client to the originating pane.
- It does not launch or focus a terminal, create an SSH connection, or start a persistent daemon.
- The token, tmux target, client, path, and attach URL are not stored in SQLite or debug logs.

The browser and tmux must be on the same computer. A phone or remote computer
cannot reach the loopback listener. Dead panes and listener startup or cleanup
failures omit or invalidate only the attach action; the ordinary notification
remains usable.

## Disable notifications

Remove the `[telegram]` section and unset overrides:

```sh
unset TWM_TELEGRAM_BOT_TOKEN TWM_TELEGRAM_CHAT_ID
```

With neither credential configured, Telegram is silently disabled. For redacted
delivery diagnostics, set `TWM_HOOK_DEBUG=1` before launching Claude and inspect
`$TMPDIR/twm_hook.log`.
