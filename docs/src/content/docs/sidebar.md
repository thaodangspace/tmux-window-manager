---
title: Persistent agents sidebar
description: Read agent status at a glance in a per-window left pane, and turn it off in one line.
---

The plugin docks a narrow, always-visible left pane in every tmux window that
lists the coding agents running in each workspace with a live status glyph. It is
**on by default**, refreshes about once a second while visible, and does
near-zero work while hidden. It never takes focus, never receives keystrokes, and
never appears in the picker's search or preview.

:::note[Turn it off in one line]
Set `enabled = false` under `[sidebar]` in `~/.config/twm.toml`, or run
`tmux-window-manager sidebar disable` for the current tmux server. See
[Disable or hide the sidebar](#disable-or-hide-the-sidebar).
:::

## Status glyphs

| Glyph | Meaning |
| --- | --- |
| `⋮ working` (cyan) | The agent is processing a prompt or turn |
| `● waiting` (yellow) | The agent needs your input (e.g. a permission prompt) |
| `✓ idle` (green) | The agent has a hook status row but is idle |
| `○ idle` (dim) | No hook status is available (for example, `pi`) |

Status comes from the same event-driven database the picker reads. An agent with
no hooks installed still appears (detected as a process), shown with the dim
`○ idle` glyph.

## Grouping and layout

Agents are grouped by **workspace** — the nearest ancestor directory containing a
`.git` entry, walking up to but never including `$HOME` (so a dotfiles `~/.git`
never swallows everything) and stopping at `/`; a git worktree's `.git` file
counts. The workspace header is the directory basename; `$HOME` shows as `~`, and
when two workspaces share a basename they are disambiguated as `name (parent)`.

Each agent row has a dim subtitle: the waiting detail if it is waiting, otherwise
the first line of the current prompt, otherwise its `session:index`. Rows in the
sidebar's own window render bold.

:::caution[Prompt excerpts are visible]
The subtitle can show the first line of an agent's current prompt, so it is on
screen while you share your terminal. Disable the sidebar before screen-sharing
if that is a concern.
:::

When rows exceed the pane height, waiting rows are kept first and a `+N more` line
is shown. With nothing running, the pane shows a single dim `no agents running`
line.

## Behavior

- **Display only.** Focus bounces off the sidebar; it never receives keystrokes.
- **Zoom hides it.** Zooming a real pane naturally hides the sidebar; the sidebar
  itself cannot stay zoomed.
- **Self-healing.** Killing the sidebar pane recreates it on the next layout
  change. If the last real pane exits, leaving only the sidebar, the window is
  closed rather than left orphaned.
- **Isolated from the picker.** Sidebar panes never match `list --query`, never
  appear as picker rows, and never stack in the preview.

## Configure width and refresh

The `[sidebar]` section of `twm.toml` (`~/.config/twm.toml`, or
`$XDG_CONFIG_HOME/twm.toml`) tunes the sidebar. Defaults are shown:

```toml
[sidebar]
enabled = true       # default true
width = 32           # columns, clamped 20..80
refresh_ms = 1000    # refresh interval while visible, clamped 500..10000
```

The effective width is `min(width, window_width / 2)`, and no sidebar is created
in a window narrower than about 10 columns. A running sidebar re-reads `width` and
`refresh_ms` when the file changes.

:::note[Re-source after changing width]
The layout guard that keeps the sidebar docked bakes in the width at install
time, so after changing `width` re-source the plugin (or run
`tmux-window-manager sidebar install`) to update it.
:::

Precedence is: the runtime tmux option `@twm_sidebar_enabled` (set by
`sidebar enable`/`disable`) over `twm.toml` over the built-in defaults.

## Disable or hide the sidebar

- **Permanently:** set `enabled = false` in the `[sidebar]` block above.
- **For this tmux server** (until it restarts): `tmux-window-manager sidebar disable`;
  `tmux-window-manager sidebar enable` turns it back on.
- **In one window:** `tmux-window-manager sidebar toggle` (pass `-t <window-id>`
  to target another window).

## Troubleshooting

- **Repair a window:** `tmux-window-manager sidebar ensure` reconciles the sidebar
  for all windows (or one with `-t <window-id>`); it is idempotent, so a second
  run is a no-op.
- **Remove everything:** `tmux-window-manager sidebar uninstall` unsets the
  sidebar's tmux hooks and kills all sidebar panes.
- **See hook errors:** the lifecycle hooks run `ensure` through `run-shell`, which
  always exits 0 but writes any error to stderr, so tmux surfaces it when you run
  the command manually.
- **tmux-resurrect:** a restored session may bring back sidebar panes as plain
  panes; `ensure` recognizes them by their start command and de-duplicates them.

## Requirements and known limits

- Requires tmux **3.2 or newer** (already needed for the picker popup).
- The layout guard's width is fixed at install time — re-source after changing
  `width` (see above).
- Hookless agents such as `pi` always show `○ idle`; the sidebar does not infer
  status from CPU usage.
