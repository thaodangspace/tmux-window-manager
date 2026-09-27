---
title: Commands and configuration
description: Reference the public commands, tmux options, paths, and environment variables.
---

## Commands

| Command | Purpose |
| --- | --- |
| `run [client]` | Open the popup and switch the launching client to the selected target |
| `list [--query TEXT]` | Emit grouped fzf rows, optionally preserving headers for matching rows |
| `install-hooks [--claude] [--codex] [--dry-run]` | Merge Claude hooks and/or print the Codex notify snippet |
| `status [--all]` | Print recorded status rows; `--all` includes dead processes |
| `sidebar enable` / `sidebar disable` | Turn the persistent agents sidebar on/off for the tmux server |
| `sidebar toggle [-t window]` | Hide or show the sidebar in one window |
| `sidebar ensure [-t window]` | Idempotently reconcile the sidebar for one or all windows |
| `sidebar install` / `sidebar uninstall` | Wire or remove the sidebar's tmux hooks and panes |
| `completion <shell>` | Generate Cobra shell completion |

The sidebar is on by default and normally reconciles itself through tmux hooks;
see the [Agents sidebar](/sidebar/) guide for configuration and troubleshooting.

The binary also re-invokes hidden implementation commands. They are documented
for troubleshooting and integrations but normally should not be called by hand:

| Internal command | Purpose |
| --- | --- |
| `popup [client]` | Run fzf inside the tmux popup and write its selection handoff |
| `preview <target>` | Render pane previews for an fzf target |
| `label <pid> [fallback]` | Resolve the nearest coding-agent process name for a status-bar label |
| `hook [event] [--agent NAME] [--codex]` | Normalize and record an agent lifecycle payload |
| `sidebar render` | Run the in-pane sidebar render loop (started by `sidebar ensure`) |

Use `tmux-window-manager <command> --help` for current argument and flag details.

## tmux options

| Option | Default | Purpose |
| --- | --- | --- |
| `@twm_key` | `w` | Prefix key that opens the picker |
| `@twm_bin` | Set automatically | Absolute binary path published by the plugin entrypoint |
| `@twm_sidebar_enabled` | Unset | Runtime sidebar toggle set by `sidebar enable`/`disable` (`1`/`0`); overrides `twm.toml` |
| `@twm_sidebar_off` | Unset | Per-window flag set by `sidebar toggle` (`1` hides the sidebar in that window) |

Override the key before loading the plugin:

```text
set -g @twm_key 'W'
set -g @plugin 'thaodangspace/tmux-window-manager'
```

Use the published binary path in a status-bar format:

```text
setw -g window-status-format "#I: #(basename '#{pane_current_path}')/#(#{@twm_bin} label #{pane_pid} #{pane_current_command})"
```

## Files and environment

| Setting | Default | Effect |
| --- | --- | --- |
| `TWM_DB_PATH` | XDG state path | Override the complete SQLite database path |
| `XDG_STATE_HOME` | `~/.local/state` | Change the base directory for `tmux-window-manager/agents.db` |
| `XDG_CONFIG_HOME` | `~/.config` | Change the directory containing optional `twm.toml` |
| `TWM_TELEGRAM_BOT_TOKEN` | File value | Non-empty Telegram bot-token override |
| `TWM_TELEGRAM_CHAT_ID` | File value | Non-empty Telegram chat-ID override |
| `TWM_HOOK_DEBUG` | Disabled | Enable redacted hook diagnostics in `$TMPDIR/twm_hook.log` |

The popup selection handoff also uses short-lived files under `$TMPDIR`. Client
names are sanitized for filenames, and the files are removed after the outer
command reads them.
