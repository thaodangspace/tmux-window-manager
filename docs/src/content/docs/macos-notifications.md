---
title: macOS notifications
description: Show Notification Center alerts that jump to the agent's tmux pane when clicked.
---

On macOS, TWM can post the same "needs input" and "finished" events as
[Telegram](/telegram/) to Notification Center. Like Telegram, it needs the
[model judge](/notification-poller/) (`[poller] enabled = true`). Clicking a notification switches
your most recently active tmux client to the agent's pane and brings your
terminal app to the front.

It uses [`alerter`](https://github.com/vjeantet/alerter). `osascript`
notifications cannot run an action on click, and `terminal-notifier` 2.0 no
longer reports clicks on current macOS.

## Set up

Install `alerter`:

```sh
brew install alerter
```

Enable the backend in `~/.config/twm.toml` (or `$XDG_CONFIG_HOME/twm.toml`):

```toml
[macos]
enabled = true
```

Notifications appear under **Terminal** (alerter's sender). If nothing shows
up, allow notifications for Terminal in System Settings → Notifications.

The macOS backend works on its own or alongside `[telegram]`. Each backend
applies its own filters.

## Options

The defaults are shown:

```toml
[macos]
enabled = true
# App activated on click. Defaults to the terminal that started the tmux
# server ($__CFBundleIdentifier), e.g. com.googlecode.iterm2,
# com.mitchellh.ghostty, net.kovidgoyal.kitty, com.github.wez.wezterm.
# terminal_bundle_id = "com.googlecode.iterm2"
# Sound for "needs input"; "" is silent. "Finished" is always silent.
sound = "default"
# Close an unclicked notification after this many seconds; 0 keeps it until
# it is clicked or dismissed.
timeout_seconds = 43200
# Same filters as Telegram:
include_response = true
min_turn_seconds = 30
skip_when_focused = true
```

To find an app's bundle ID, run `osascript -e 'id of app "Ghostty"'`.

## What happens on click

`alerter` reports a click only while it is still running, so the watcher starts
a small detached `tmux-window-manager notify-wait` process per notification and
moves on. When the notification is clicked, `notify-wait`:

1. checks that the pane still exists,
2. runs `switch-client` on the attached client with the most recent activity,
   targeting the pane's session, window, and pane,
3. raises the terminal with `open -b <terminal_bundle_id>`.

The tmux socket is the one the plugin passed to the watcher, so non-default
servers (`tmux -L`) work too. Dismissing the notification or reaching the timeout ends
the waiter without doing anything.

A newer event for the same pane replaces the older notification and stops its
waiter, so at most one waiter runs per pane. The same jump
is available by hand as `tmux-window-manager jump [--socket PATH] <pane-id>`.

## Troubleshooting

Set `TWM_DEBUG=1` in the tmux server environment and restart the watcher.
`$TMPDIR/twm_debug.log` then records `alerter-missing`, delivery failures, jump errors, or why an event was
skipped. Remove the `[macos]` section, or set `enabled = false`, to turn the
backend off.
