# tmux-window-manager

A fuzzy window switcher for tmux (`prefix + w`), as a self-contained Go binary
distributed as a [TPM](https://github.com/tmux-plugins/tpm) plugin. It lists
every window across all sessions, previews their panes, and badges windows
running coding agents (Claude Code, Codex, pi, Gemini CLI, OpenCode, Aider, and
more) — with a busy spinner and the agent's model — all detected **natively**,
with no external tools beyond `tmux` and `fzf`.

It is a Go port of a personal `tmux_window_manager.sh` script; the port drops the
script's `jq` / `awk` / `fd` / `t2` dependencies in favor of native Go.

![tmux-window-manager fuzzy picker with coding-agent status and pane preview](docs/SCR-20260724-pusx.png)

<p align="center">
  <img src="docs/SCR-20260724-pwqs.png" alt="Responsive tmux-window-manager picker on a narrow mobile display" width="360">
  <br>
  <em>Responsive narrow layout with the pane preview hidden.</em>
</p>

## Features

- **Fuzzy picker** across all sessions, grouped by session with a live preview
  of each window's panes.
- **Agent status from the panes** — a background watcher started by the plugin
  captures every pane running a coding agent. A changing screen shows as
  **working** (`⟳`); a settled screen is idle, or, with an optional local model
  (LM Studio), **waiting on you** (`🔔`), finished, or failed. No per-agent
  setup or hooks.
- **Optional Telegram notifications** — best-effort messages when the model
  judges that an agent needs input or finished a turn.
- **Optional macOS notifications** — the same events in Notification Center via
  `alerter`; clicking one jumps to the agent's tmux pane.
- **Agent model label** — with the model judge on, the AI model name printed on
  the agent's screen is shown next to the agent in the picker.
- **Ctrl-N** — create/attach a session in a directory you pick or type.
- **Ctrl-X** — kill the highlighted row's session (if it is the one you are
  attached to, you are moved to another session first; the last session is
  never killed).
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
set -g @plugin 'thaodangspace/tmux-window-manager'
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

## Documentation

The full user guide is an Astro/Starlight site under [`docs/`](docs/README.md).
Run it locally or build its static output with:

```bash
make docs-dev
make docs-build
```

The docs include picker controls, agent status, the model judge, troubleshooting, and
Cloudflare Pages deployment settings.

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
| `uninstall-hooks [--dry-run]` | Remove the agent hooks older releases installed |
| `watch [--detach] [--replace] [--once]` | Pane watcher (started by the plugin); `--once` prints the model's verdicts |
| `status [--all]` | Dump the recorded agent status rows (debug) |

## Agent status

Nothing to install per agent. When tmux loads the plugin, it starts
`tmux-window-manager watch` in the background (restarting it after each
rebuild). Every 5 seconds the watcher captures each pane running a detected
agent: a screen that changed is `working`; a screen unchanged for 6 seconds is
`idle`. Digits are ignored, so timers and token counters do not count as
change. With the optional [model judge](#model-judge-optional) a settled screen
is classified as waiting, finished, failed, or idle instead, and only then are
notifications sent.

Agents are detected by process name, including agents launched through an
interpreter (`node /opt/homebrew/bin/gemini` → `gemini`): `claude`, `codex`,
`pi`, `gemini`, `opencode`, `cursor-agent`, `aider`, `amp`, `goose`, `qwen`,
`crush`, `droid`, `copilot`.

**Upgrading from a release that used hooks?** Run once:

```bash
tmux-window-manager uninstall-hooks
```

It removes the twm hooks from `~/.claude/settings.json` and prints the line
number of the twm `notify = [...]` line in `~/.codex/config.toml` for you to
delete. Until then the old hooks call a no-op and exit 0.

## Telegram notifications (optional)

Copy the included example and replace both values:

```bash
mkdir -p ~/.config
cp twm.toml.example ~/.config/twm.toml
chmod 600 ~/.config/twm.toml
```

```toml
[telegram]
bot_token = "<bot-token>"
chat_id = "<chat-id>"
```

Notifications require the [model judge](#model-judge-optional): TWM sends a
message when the model judges an agent as needing input (or failed) or as
finished. Delivery is best effort. Environment variables
`TWM_TELEGRAM_BOT_TOKEN` and `TWM_TELEGRAM_CHAT_ID` can override the file.
Messages show the tmux `session:window`, model, the model's summary, and turn
duration. Short turns and panes you are watching are skipped by default;
`include_response`, `min_turn_seconds`, and `skip_when_focused` tune this.
See the [Telegram guide](docs/src/content/docs/telegram.md) for details.

## macOS notifications (optional)

```bash
brew install alerter
```

```toml
[macos]
enabled = true
```

The same events appear in Notification Center. Clicking one switches your most
recent tmux client to the agent's pane and activates your terminal. It uses the
same filter keys as Telegram, plus `terminal_bundle_id`, `sound`, and
`timeout_seconds`. See the
[macOS guide](docs/src/content/docs/macos-notifications.md).

## Model judge (optional)

A local model reads each settled agent screen and decides its state. With LM
Studio serving `google/gemma-4-12b` on `localhost:1234`:

```toml
[poller]
enabled = true
```

The watcher sends the last 60 lines of a screen that stopped changing to the
model, which returns `working`, `waiting`, `completed`, `error`, or `idle`, a
one-sentence summary, and the model name printed on screen. The verdict becomes
the picker status, and waiting / completed / error are delivered through the
Telegram/macOS backends above, once per settled screen. Starting the watcher is
silent. Without the judge there are no notifications.
`tmux-window-manager watch --once` prints the model's verdict for every agent
pane. Reload tmux after editing `[poller]`. See the
[watcher guide](docs/src/content/docs/notification-poller.md).

## Notes

- **Status source.** Agent presence and name come from the process table; model
  and status come from the status DB at
  `~/.local/state/tmux-window-manager/agents.db` (override with `$TWM_DB_PATH`;
  honors `$XDG_STATE_HOME`), which only the watcher writes. Rows are keyed per
  pane and carry the agent PID, so a crashed agent's badge clears automatically;
  the watcher also clears and rebuilds every row when it starts. Badges lag the
  screen by at most one scan, the settle time, and one model call. Set
  `TWM_DEBUG=1` in the tmux environment to log redacted watcher and delivery
  errors to `$TMPDIR/twm_debug.log`.
- The directory picker (`Ctrl-N`) does not honor `.gitignore` (the original
  relied on `fd` for that); explicit excludes cover `.git`, `node_modules`,
  `Library`, `.Trash`.

## License

MIT — see [LICENSE](LICENSE).
