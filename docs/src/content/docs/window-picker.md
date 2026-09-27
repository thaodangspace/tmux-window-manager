---
title: Window picker
description: Search windows, preview panes, create sessions, and open project directories.
---

Open the picker with `prefix + w`. Windows are grouped by tmux session. Each
visible window row mirrors the status-label shape:
`project-directory/agent-or-command`, followed by an optional agent status.
Session and window identifiers, commands, paths, and model-enriched labels remain
searchable even when they are not all displayed.

## Controls

| Key | Action |
| --- | --- |
| `Enter` | Switch the launching tmux client to the selected session or window |
| `Ctrl-R` | Reload rows and read the latest status database state |
| `Ctrl-A` | Toggle an agents-only view: show just the windows running a coding agent (and the sessions that contain one) |
| `Ctrl-N` | Pick or type a directory, then create or attach to a session |
| `Ctrl-X` | Kill the selected row's session; if it is your current session you are switched to another one first, and the last session is never killed |
| `Esc` / `Ctrl-C` | Cancel without switching |

`Ctrl-A` flips between every window and a focused agents-only view. It drops
windows with no detected coding agent and any session left without one, and it
combines with the current search query. The toggle resets to "off" each time the
popup opens.

The preview shows stacked panes for the highlighted window and labels recognized
coding-agent processes. Clients narrower than 100 columns omit the preview to
preserve space. If the client width cannot be read, the preview stays enabled.

## Create or attach to a session

Press `Ctrl-N` from the main picker. Directory suggestions are repositories with
a direct `.git` entry under the configured search roots. You can also type a
path that was not suggested; missing directories are created.

- If the target session exists, the plugin adds a window in the chosen directory.
- Otherwise, it creates a detached session in that directory and switches the launching client to it.
- A typed main-picker query becomes the session name. Without one, the directory basename is used.
- Colons in generated session names are replaced with hyphens.

:::note[Directory discovery]
The repository walk uses explicit excludes such as `.git`, `node_modules`,
`Library`, and `.Trash`; it does not interpret `.gitignore` files.
:::

## Why switching happens after the popup closes

A `switch-client` issued inside a tmux popup is undone when that popup exits.
The plugin therefore records the fzf selection in client-specific temporary
files, closes the popup, and lets the outer `run` command perform the switch.
Those handoff files are removed after use.
