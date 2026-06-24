# tmux-window-manager

A fuzzy window switcher for tmux (`prefix + w`), as a self-contained Go binary
distributed as a [TPM](https://github.com/tmux-plugins/tpm) plugin. It lists
every window across all sessions, previews their panes, and badges windows
running coding agents (`claude` / `codex` / `pi`) — with a busy spinner and the
agent's model — all detected **natively**, with no external tools beyond `tmux`
and `fzf`.

It is a Go port of a personal `tmux_window_manager.sh` script; the port drops the
script's `jq` / `awk` / `fd` / `t2` dependencies in favor of native Go.

## Features

- **Fuzzy picker** across all sessions, grouped by session with a live preview
  of each window's panes.
- **Event-driven agent status** — agents push their lifecycle state into a small
  SQLite DB via Claude Code / Codex hooks, so the picker badges each window as
  **working** (`⟳`), **waiting on you** (`🔔`, e.g. a permission prompt), or idle
  — no process polling or pane-scraping. Run `tmux-window-manager install-hooks`
  once to wire it up.
- **Agent preview** — model and latest message, captured at hook time from
  Claude transcripts (`~/.claude/projects/**/*.jsonl`) and the Codex notify
  payload.
- **Ctrl-N** — create/attach a session in a directory you pick or type.
- **Ctrl-Z / Ctrl-T** — open Zed / Typora on the highlighted window's directory.
- **Ctrl-R** — reload the list (re-reads the latest status).
- **Status-bar label** — `tmux-window-manager label <pid> <fallback>` prints a
  pane's agent name for `window-status-format`.

## Requirements

- **tmux ≥ 3.2** (for `display-popup`)
- **fzf** — the picker UI
- **Go ≥ 1.25** — the plugin builds its binary on install (the pure-Go SQLite
  driver requires it; still cgo-free, no C toolchain needed)
- macOS or Linux

## Install (TPM)

Add to `~/.tmux.conf`:

```tmux
set -g @plugin 'dtonair/tmux-window-manager'
```

Then press `prefix + I`. TPM clones the repo and the plugin's `.tmux` hook builds
the binary (`go build`) on first run, rebuilding automatically when the source
changes. Press `prefix + w` to open the picker.

### Local install (no GitHub)

Point tmux at a local checkout instead of cloning:

```tmux
run-shell ~/code/tmux-window-manager/tmux-window-manager.tmux
```

This runs the same build-on-install hook and key binding.

### Build from source

```bash
go build -o bin/tmux-window-manager ./cmd/tmux-window-manager
```

## Configuration

| Option        | Default | Purpose                                              |
|---------------|---------|------------------------------------------------------|
| `@twm_key`    | `w`     | Prefix key that opens the picker                     |
| `@twm_bin`    | (auto)  | Set by the plugin to the built binary path, so other config can call it |

Use `@twm_bin` to add the agent label to your status bar:

```tmux
setw -g window-status-format "#I: #(basename '#{pane_current_path}')/#(#{@twm_bin} label #{pane_pid} #{pane_current_command})"
```

## Commands

The binary re-invokes itself for its internal modes; you normally only bind
`run`, but all subcommands are usable:

| Command | Purpose |
|---------|---------|
| `run [client]` | Open the picker popup and switch to the selection |
| `list` | Emit the window rows fzf consumes |
| `preview <target>` | Render a window's panes |
| `label <pid> [fallback]` | Print a pane's agent name (status bar) |
| `open-editor <zed\|typora> <target>` | Open an editor on a window's path |
| `install-hooks [--claude] [--codex] [--dry-run]` | Wire status hooks into Claude Code / Codex |
| `hook [event]` | Record an agent lifecycle event (called by the hooks) |
| `status [--all]` | Dump the recorded agent status rows (debug) |

## Agent status setup

Run once to wire the hooks into Claude Code and print the Codex snippet:

```bash
tmux-window-manager install-hooks
```

This idempotently merges `SessionStart` / `UserPromptSubmit` / `Notification` /
`Stop` / `SessionEnd` hooks into `~/.claude/settings.json` (preserving your own
hooks) and prints a `notify = [...]` line to add to `~/.codex/config.toml`. From
then on, each agent reports its status as it works, and the picker reflects it.

## Notes

- **Status source.** Agent presence, name, model, and status all come from the
  status DB at `~/.local/state/tmux-window-manager/agents.db` (override with
  `$TWM_DB_PATH`; honors `$XDG_STATE_HOME`). Rows are keyed by working directory
  and carry the agent PID, so a crashed agent's badge clears automatically (its
  process is gone) even if no `SessionEnd` fired. An agent with no hooks
  installed simply shows no badge. Set `TWM_HOOK_DEBUG=1` to log hook activity to
  `$TMPDIR/twm_hook.log`.
- The directory picker (`Ctrl-N`) does not honor `.gitignore` (the original
  relied on `fd` for that); explicit excludes cover `.git`, `node_modules`,
  `Library`, `.Trash`.

## License

MIT — see [LICENSE](LICENSE).
