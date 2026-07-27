---
title: Agent status
description: Configure event-driven Claude and Codex lifecycle badges.
---

Agent status is event-driven. Hooks invoke `tmux-window-manager hook`, and each
event updates a local SQLite row. The picker reads that database when it opens or
when you press `Ctrl-R`; it does not scrape pane text or poll transcripts.

## Install supported hooks

Run the installer using the binary built by TPM or from source:

```sh
tmux-window-manager install-hooks
```

By default it handles both integrations:

- **Claude Code:** idempotently merges `SessionStart`, `UserPromptSubmit`,
  `Notification`, `Stop`, and `SessionEnd` commands into
  `~/.claude/settings.json`. Existing non-plugin settings and hooks are preserved.
- **Codex:** prints the `notify = [...]` line to add manually to
  `~/.codex/config.toml`.

Preview without changing Claude settings:

```sh
tmux-window-manager install-hooks --claude --dry-run
```

Select only one integration with `--claude` or `--codex`. With neither flag, both
are selected. Re-running the installer does not duplicate its Claude hooks.

## Status meanings

| State | Picker meaning |
| --- | --- |
| Working (`⟳`) | The agent is processing a prompt or turn |
| Waiting (`🔔`) | The agent emitted a notification and needs user attention |
| Idle / no badge | No live status row currently applies to the window directory |

Agent names and models are captured with lifecycle data. Claude model and latest
text are read with a bounded transcript tail; the first user prompt comes from
the prompt event. Codex uses fields in its notify payload.

Process detection can recognize Claude, Codex, and Pi for pane labels and
previews. The automatic hook installer configures Claude and Codex only.

## Storage and cleanup

The default database is:

```text
~/.local/state/tmux-window-manager/agents.db
```

`$XDG_STATE_HOME` changes the state root, and `$TWM_DB_PATH` overrides the full
path. SQLite uses WAL mode and a busy timeout for concurrent, short-lived hook
writers.

Rows include the resolved agent process ID. Normal reads hide and lazily remove
rows whose process is no longer alive, so a crashed agent clears even when a
session-end event was missed.

Inspect live rows with:

```sh
tmux-window-manager status
```

Include dead rows for debugging with `status --all`.

:::important[Hooks never block the agent]
The hook command always exits successfully. Database, payload, transcript, or
optional notification failures cannot block Claude or Codex. Set
`TWM_HOOK_DEBUG=1` before launching the agent to write redacted diagnostics to
`$TMPDIR/twm_hook.log`.
:::
