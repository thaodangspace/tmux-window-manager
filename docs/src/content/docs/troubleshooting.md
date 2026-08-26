---
title: Troubleshooting
description: Diagnose build, picker, hook, and status problems.
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

1. Run `tmux-window-manager install-hooks` and complete the printed Codex step if needed.
2. Start a new agent process so it inherits current environment settings.
3. Submit a prompt, then inspect `tmux-window-manager status`.
4. Press `Ctrl-R` in the picker to reload the database.
5. Confirm the agent and tmux pane use the same working directory.

Set `TWM_HOOK_DEBUG=1` before launching the agent to enable redacted diagnostics.
Hook failures always return success to the agent, so the agent UI may not show
the underlying status-write problem.

If `$TWM_DB_PATH` or `$XDG_STATE_HOME` differs between the agent and tmux server,
they may read different databases. Make those variables consistent before
starting each process.

## A stale status row is visible

Normal picker and `status` reads verify the stored process ID and lazily remove
dead rows. Use `status --all` to inspect all database rows. If a PID is still
alive, the row remains eligible; end the originating agent session normally or
investigate the recorded PID before deleting the database.
