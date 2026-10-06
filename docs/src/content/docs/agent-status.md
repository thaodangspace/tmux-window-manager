---
title: Agent status
description: How the pane watcher detects coding agents and badges their windows.
---

Agent status comes from the panes themselves. When tmux loads the plugin, it
starts `tmux-window-manager watch` in the background. The watcher captures every
pane running a coding agent, writes one status row per pane to a local SQLite
database, and the picker reads that database when it opens or when you press
`Ctrl-R`. There is nothing to install per agent.

## Supported agents

Agents are found by walking each pane's process tree. These process names are
recognized:

`claude`, `codex`, `pi`, `gemini`, `opencode`, `cursor-agent`, `aider`, `amp`,
`goose`, `qwen`, `crush`, `droid`, `copilot`

Agents launched through an interpreter (`node`, `bun`, `deno`, `python`,
`python3`, `python3.x`) are matched by their script name without extension, so
`node /opt/homebrew/bin/gemini` is detected as `gemini`.

## How status is decided

Every 5 seconds the watcher captures the last 60 lines of each agent pane and
compares it with the previous scan. Digits are ignored, so elapsed-time and
token counters do not count as change.

- **Screen changed** → `working`.
- **Screen unchanged for 6 seconds** → judged once:
  - with the [model judge](/notification-poller/) enabled, a local model reads
    the screen and returns `working`, `waiting`, `completed`, `error`, or
    `idle`, plus a one-sentence summary and the model name if one is printed
    on screen;
  - without it, the pane becomes `idle`.
- A screen the model judged `working` that stays unchanged (a long silent tool
  call) is re-judged every 2 minutes.

Badges therefore lag the screen by at most one scan, the settle time, and one
model call. Scan, settle, and re-judge timings are configurable in
[`[poller]`](/notification-poller/#configuration).

## Status meanings

| State | Picker meaning |
| --- | --- |
| Working (`⟳`) | The agent's screen is changing, or the model says it is still running |
| Waiting (`🔔`) | The model says the agent needs you: a permission prompt, a question, a choice |
| Error | The model says the agent stopped on a failure |
| Idle / no status | Settled screen: turn finished, empty prompt, or no model verdict |

Waiting and error require the model judge. A `completed` verdict is shown as
idle; it only matters for [notifications](/notification-poller/). The model name
the judge reads from the screen appears next to the agent name in the picker.

## Upgrading from a release that used hooks

Older releases recorded status through Claude Code hooks and the Codex `notify`
program, set up by a hook installer that no longer exists. Remove them once:

```sh
tmux-window-manager uninstall-hooks --dry-run   # preview
tmux-window-manager uninstall-hooks
```

It removes every twm hook group from `$CLAUDE_CONFIG_DIR/settings.json` (or
`~/.claude/settings.json`), keeping your other settings and hooks, and prints
the line number of the twm `notify = [...]` line in `$CODEX_HOME/config.toml`
(or `~/.codex/config.toml`) for you to delete by hand. Until you do, old hooks
call a no-op command that exits successfully.

## Storage and cleanup

The default database is:

```text
~/.local/state/tmux-window-manager/agents.db
```

`$XDG_STATE_HOME` changes the state root, and `$TWM_DB_PATH` overrides the full
path. Only the watcher writes it. Rows are keyed per pane (`pane:%N`) and carry
the agent's process ID; normal reads hide rows whose process is no longer
alive. When the watcher starts it clears every row and rebuilds them from the
panes, and it removes the row of a pane that no longer runs an agent.

Inspect live rows with:

```sh
tmux-window-manager status
```

Include dead rows for debugging with `status --all`.

:::tip[Debug log]
Set `TWM_DEBUG=1` in the tmux server environment and restart the watcher to log
redacted diagnostics (scans, verdicts, delivery failures) to
`$TMPDIR/twm_debug.log`.
:::
