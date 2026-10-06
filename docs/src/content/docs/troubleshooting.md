---
title: Troubleshooting
description: Diagnose build, picker, watcher, and status problems.
---

## The plugin does not build

Confirm the required Go version and build directly to expose the compiler error:

```sh
go version
go build -o bin/tmux-window-manager ./cmd/tmux-window-manager
```

The plugin entrypoint rebuilds when the binary is missing or older than any Go
source file. Go 1.25 or newer is required by the pure-Go SQLite dependency.

## The popup does not open

Check that tmux is at least 3.2 and that `fzf` is available to the tmux server:

```sh
tmux -V
command -v fzf
```

Restart the tmux server after changing its environment. The CLI prepends common
Homebrew locations (`/opt/homebrew/bin` and `/usr/local/bin`), but custom install
locations must still be visible through `PATH`.

The `run` command intentionally treats a popup that produced no selection as a
no-op. Run the binary manually inside tmux when diagnosing startup failures.

## Agent badges do not appear

1. Confirm the agent is one of the [supported agents](/agent-status/#supported-agents)
   and runs inside a tmux pane. `tmux-window-manager label <pane_pid>` prints
   the detected name.
2. Make sure the watcher runs: `pgrep -fl 'tmux-window-manager watch'`. Reload
   tmux, or run `tmux-window-manager watch --detach --replace`, to start it.
3. Inspect `tmux-window-manager status`, then press `Ctrl-R` in the picker to
   reload the database.

Status lags the screen by up to a scan, the settle time, and one model call.
Without `[poller] enabled = true` you only see working and idle, never waiting.

Set `TWM_DEBUG=1` in the tmux server environment (`tmux setenv -g TWM_DEBUG 1`)
and restart the watcher to log redacted diagnostics to `$TMPDIR/twm_debug.log`.

If `$TWM_DB_PATH` or `$XDG_STATE_HOME` differs between the tmux server and your
shell, the picker and the watcher may use different databases. Keep those
variables consistent.

## Waiting badges or notifications never appear

1. Check that `[poller] enabled = true` is set and the watcher was restarted
   after the edit.
2. Run `tmux-window-manager watch --once`. An `error:` column means the model
   endpoint is unreachable or returned an unusable verdict; check that LM
   Studio's server is running with the configured model loaded.
3. Screens already on display when the watcher starts never notify, and each
   settled screen notifies once. `skip_when_focused` and `min_turn_seconds`
   can also skip a notification; the debug log records why.

## Agents still call an old hook

Releases that used Claude Code and Codex hooks left entries in your agent
configuration. They now call a no-op and do no harm, but remove them with
`tmux-window-manager uninstall-hooks` (see [Agent status](/agent-status/#upgrading-from-a-release-that-used-hooks)).

## A stale status row is visible

Normal picker and `status` reads verify the stored process ID and lazily remove
dead rows. Use `status --all` to inspect all database rows. The watcher deletes
the row of a pane that no longer runs an agent and rebuilds every row when it
starts, so restarting it (`watch --detach --replace`) clears anything stale.
