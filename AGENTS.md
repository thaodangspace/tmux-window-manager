# AGENTS.md

Cached architecture notes for `tmux-window-manager`. Read this before changing
code; update it when you introduce new modules, commands, or design decisions.

## What this is

A self-contained Go port of a `tmux_window_manager.sh` dotfiles script: a fuzzy
tmux window switcher (`prefix + w`) shipped as a TPM plugin that builds its
binary on install. The port replaces the script's `jq` / `awk` / `fd` / `t2`
dependencies with native Go; the only external command dependencies are `tmux`
and `fzf`.

## Layout

```
cmd/tmux-window-manager/main.go   entrypoint -> cli.Execute()
cli/                              cobra command tree (one file per subcommand)
  root.go      command wiring, exit codes, PATH priming hook
  path.go      ensurePath(): prepend /opt/homebrew/bin etc (run-shell has a minimal env)
  run.go       outer launcher: popup -> read selection -> switch / new session
  popup.go     inside the popup: fzf, writes selection temp files
  list.go      emit fzf rows (live enricher reads the status DB)
  preview.go   fzf preview
  label.go     status-bar agent name (process detection)
  hook.go      record an agent lifecycle event -> status DB (always exits 0)
  installhooks.go  merge hooks into ~/.claude/settings.json + Codex snippet
  status.go    debug dump of the status rows
  sidebar.go   sidebar command group: ensure/install/uninstall/enable/disable/toggle
               + index-90 hook table, geometry guard, focus bounce, wake
  sidebar_render.go  hidden `sidebar render`: the in-pane render loop entrypoint
tmuxcli/    typed wrappers over the tmux CLI (one ps/tmux call shape per func)
  sidebar.go     sidebar pane detection/split/kill/resize + hook set/unset;
                 id-validated, paired pure ...Args builders, NotSidebarFilter
agents/     agent detection + hook payload normalization
  registry.go    Kind{ID,Display} registry (claude/codex/pi) + IsAgent/DisplayName
  proc.go        process-subtree walk -> agent names (port of the awk);
                 NearestAgent() ancestor walk for hook pid resolution;
                 Agents()/AgentGroups() outermost-agent-per-chain (sidebar)
  transcript.go  Claude .jsonl tail reader (model/latest) + parse helpers
  hookpayload.go normalize Claude (stdin) / Codex (notify) payloads -> status
store/      SQLite status persistence (the event-driven status source)
  path.go        canonical DB path ($TWM_DB_PATH / $XDG_STATE_HOME / ~/.local/state)
  store.go       schema, Upsert/Get/Delete, LiveByCwd (pid-liveness + lazy reap)
  version.go     DataVersion() (PRAGMA data_version) for the sidebar change-skip
  alive.go       kill(pid,0) liveness (unix)
config/     twm.toml [sidebar] loader: defaults, clamps, runtime>file precedence
  sidebar.go     Load/LoadFrom/Path/ResolveEnabled; decodes only [sidebar]
notify/     optional Telegram config, safe message composition, and HTTP delivery
picker/     list row building + fzf invocation
  build.go     rows; Enricher iface; visible status-panel-label/bot/status format
  enrich.go    LiveEnricher: pane paths + status map -> badges (pure lookups)
  color.go     ANSI palette (robot icon + running/waiting status text)
  fzf.go       fzf option assembly, ShellQuote, selection temp-file paths
dirs/       native Git-repo directory lister for Ctrl-N (replaces fd)
  list.go        GitRoot(path): nearest ancestor with .git, stops before $HOME
sidebar/    the persistent agents panel: pure pieces + the render loop
  collect.go     Collect(): panes + ps + live DB rows -> grouped Snapshot
  render.go      Render()/Frame(): Snapshot -> width/height-bounded ANSI lines
  sanitize.go    Sanitize(): strip C0/C1/ESC/DEL before any external text renders
  ensure.go      Decide(): pure geometry/lifecycle -> Create/Kill/Resize/... actions
  lock_unix.go   Lock(): $TMPDIR flock (O_NOFOLLOW, 0600) serializing ensure
  loop.go        Loop(): tick/visibility/signals, diffed frame writes
preview/    preview rendering (stacked panes; pane-names via process detection)
docs/       isolated Astro/Starlight static documentation site
  src/content/docs/ user guides and reference pages
  src/assets/       docs-owned copies of picker screenshots
  public/_headers   Cloudflare Pages response headers
  plans/            pre-existing implementation plans (not Astro content)
tmux-window-manager.tmux   TPM entry: build-on-install + bind key + publish @twm_bin
```

## Command surface

| Subcommand | Purpose |
|------------|---------|
| `run` / `popup` / `list` / `preview` / `label` | the picker UI (ports of the original script modes) |
| `toggle-agents [client]` | flip the picker's per-client agents-only filter (Ctrl-A) |
| `hook [event] [--agent] [--codex]` | record one lifecycle event |
| `install-hooks [--claude] [--codex] [--dry-run]` | wire the hooks into Claude/Codex config |
| `status [--all]` | debug dump of the status rows |
| `sidebar ensure [-t win]` | idempotently reconcile the sidebar pane(s) for one/all windows |
| `sidebar install` / `uninstall` | register/remove the index-90 hooks + dock/kill sidebar panes |
| `sidebar enable` / `disable` | flip `@twm_sidebar_enabled` then install/uninstall |
| `sidebar toggle [-t win]` | flip per-window `@twm_sidebar_off` and reconcile |
| `sidebar render` (hidden) | the in-pane render loop; started by `ensure`, not by hand |

The binary re-invokes itself via `os.Executable()` (the script used `$BASH_SOURCE`).

## Key design decisions

- **Event-driven status, not polling.** Agents push lifecycle events (start /
  prompt / notification / stop / end) by invoking `twm hook <event>` from Claude
  Code hooks (stdin JSON) and the Codex `notify` program (argv JSON). Each firing
  upserts one row into a SQLite DB keyed by `(agent, session_id)`. The picker
  reads the DB — there is **no** `capture-pane` busy regex and **no** transcript
  JSON cache anymore (both removed). The payoff is a real **waiting-on-user**
  state (`🔔`) that pane-scraping could never detect reliably.
- **Telegram is a post-write, best-effort side effect.** With credentials in
  `~/.config/twm.toml` (or environment overrides), exact `Notification` and
  `Stop` hook events send a bounded Telegram Bot API request after the status
  write succeeds. Other events and Codex notify payloads are excluded. Delivery
  failures are redacted, never roll back status, and never fail the agent hook.
- **Telegram noise filters live in the hook.** Schema v2 adds `turn_prompt` /
  `turn_started_at` (replaced on each `UserPromptSubmit`) and `notified_at`
  (`MarkNotified` after a delivery). The hook skips `Stop` for turns shorter
  than `min_turn_seconds`, skips anything while `$TMUX_PANE` is the visible
  pane of a client with input in the last 2 minutes (`tmuxcli.LookupPane`),
  and sends Claude's `idle_prompt` reminder only when `notified_at <
  turn_started_at`. `Stop` messages go out silently; the reply excerpt is
  opt-in (`include_response`) because it leaves the machine.
- **Status source of truth: `store`.** DB at
  `~/.local/state/tmux-window-manager/agents.db` (override `$TWM_DB_PATH`, honors
  `$XDG_STATE_HOME`). WAL + `busy_timeout` for concurrent short-lived hook
  writers. Pure-Go `modernc.org/sqlite` keeps the build cgo-free (but bumps the Go
  floor to 1.25). Rows carry the resolved agent **pid**; `LiveByCwd` hides and
  lazily reaps rows whose process is dead, so a crashed agent (missed
  `SessionEnd`) clears itself. The hook handler **always exits 0** so a DB hiccup
  never blocks the agent.
- **Enrichment at hook-write time.** Model/latest come from the Codex payload or
  a bounded **tail read** of the Claude transcript (`TranscriptTail`, not a
  full-file scan); the first prompt comes straight from the `UserPromptSubmit`
  event. `proc.go` is still used for preview pane-names and the `label` command,
  but no longer drives picker status.
- **Popup → outer handoff via temp files.** `switch-client` from inside a popup
  is undone when the popup closes, so the popup writes the selection + fzf exit
  code to `$TMPDIR/tmux_wm_{sel,err}_<client>.txt` and the outer `run` acts after
  the popup closes. Enter switches targets, Ctrl-N creates a session, and Ctrl-X
  kills the session of the selected header/window row. Client `/` is sanitized
  to `_` in the filename.
- **Ctrl-X never detaches the client.** With tmux's default
  `detach-on-destroy on`, killing the attached session would drop the user out
  of tmux, so when the target is the client's current session `run` prepends a
  `switch-client` to the client's last session (else the first other session)
  in the same tmux invocation. The last remaining session is refused. Session
  targets are `=`-prefixed for exact (not prefix) matching.
- **Ctrl-A agents-only toggle.** In the picker, `Ctrl-A` flips the list between
  every window and only windows running a coding agent (plus the session headers
  that still contain one). Each fzf reload spawns a fresh `list` process that
  cannot inherit in-memory state, so the toggle lives in a per-client temp file
  (`$TMPDIR/tmux_wm_agents_<client>.txt`, `picker.AgentsFilterFile`):
  `toggle-agents` flips it and `list --client` reads it on every reload.
  `BuildFilteredAgents` applies it through `windowRow.hasAgent`, and the popup
  clears the file on open so each popup starts unfiltered.
- **Picker row display mirrors the tmux status panel.** Session headers remain
  the group label; window rows keep `session:index`, raw `window_name`, command,
  path, and model-enriched agent labels only as hidden fzf target/search terms.
  The visible row is the same shape as the status bar label —
  `basename(pane_current_path)/label` where `label` is the detected agent name or
  `pane_current_command` fallback — plus optional `🤖 - status`.
- **Responsive preview.** The popup checks the launching tmux client's width;
  clients narrower than 100 columns hide fzf's right-hand preview so the window
  list can use the full popup width. If tmux cannot report a width, the preview
  remains visible.
- **Grouped search includes preview text.** Since fzf is disabled to keep
  session headers grouped, `list --query` performs case-insensitive substring
  matching itself. Query words must occur contiguously, so typos or characters
  scattered through opaque IDs do not produce false positives. It searches
  window/session metadata first, then lazily captures the visible bodies of
  otherwise-unmatched panes so text shown in the preview can find its window
  without adding that potentially large text to the emitted fzf row.
- **Ctrl-N directory suggestions are Git repos only.** The new-session picker
  still walks the configured roots (`currentDir`, `$HOME`, `~/code`, `~/go`, and
  `$HOME` top-level children), but emits only candidates with a direct `.git`
  entry. Manually typed paths are still accepted/created by `newSession`.
- **One sidebar pane per window, funnelled through idempotent `ensure`.** The
  persistent agents panel (on by default) is a detached `split-window -hbf -l w`
  pane per window running `<bin> sidebar render`, where `w = min(cfg.Width,
  window_width/2)` and no sidebar is created under 10 cols. Lifecycle hooks never
  split directly — they `run-shell -b "<bin> sidebar ensure -t #{window_id}"`,
  serialized by a `$TMPDIR` flock (`O_NOFOLLOW`, 0600), and the pure
  `sidebar.Decide` reconciles geometry: wrong position → kill + recreate (the
  pane is stateless), wrong width → `resize-pane -x`, ≥2 sidebars → keep the
  correct one, only-sidebar-left → kill the window. A second run is a no-op, so
  parallel restore hooks converge (recount happens under the lock).
- **Sidebar panes are identified by a contains-marker, not a suffix.** tmux
  re-quotes multi-word start commands (`'<bin>' sidebar render` is stored as
  `"'<bin>' sidebar render"`), so the marker is `pane_start_command` **containing**
  ` sidebar render` — Go `IsSidebarStartCommand` (`strings.Contains`) and the tmux
  glob `#{m:* sidebar render*,#{pane_start_command}}`. This single marker drives
  detection, the picker isolation filter, and the tmux-only hooks.
- **Fixed hook index 90; never clobber user hooks.** `sidebar install` sets every
  global hook at array index `90` so a user's hooks at other indices (e.g. index
  0) survive; `uninstall` unsets exactly that index for the same names.
  `after-break-pane`/`after-move-window` are intentionally absent (they do not
  exist; break/move are covered by `window-linked`/`window-unlinked`/
  `window-layout-changed`). The TPM entry calls `install` on load, which collapses
  to a pure teardown when resolved-disabled, so it is safe to run unconditionally.
- **Geometry guard and focus bounce are tmux-only (no process spawn).** The
  high-frequency `window-layout-changed`/`window-resized`/`client-resized` hooks
  wrap `ensure` in an `if -F` format that is false when the window already has
  exactly one correctly-docked sidebar, so mouse drags do not spawn a process per
  firing (the guard bakes in the install-time width — re-source after changing
  it). `window-pane-changed` bounces focus off a sidebar with `select-pane -R`
  (`last-pane` errors with no last pane); sidebar panes have input disabled
  (`select-pane -d`).
- **Hidden sidebars sleep; wake on SIGUSR1.** The `render` loop ticks only while
  visible (window active in an attached session, not zoomed, enabled); hidden it
  stops the ticker and blocks on SIGUSR1 / SIGWINCH / a 10s fallback. Focus/session
  hooks (`session-window-changed`, `client-session-changed`, `after-select-window`)
  `run-shell -b "kill -USR1 <sidebar pids>"` so a hidden→visible flip redraws
  promptly with no twm process spawned. Visible cost stays ≲3% of a core; `ps` and
  `db.Live()` are skipped when the pane set and `PRAGMA data_version` are unchanged.
  The loop ignores SIGINT/SIGQUIT/SIGTSTP and exits cleanly (restoring the
  terminal) on SIGHUP/SIGTERM or `@twm_sidebar_enabled=0`.
- **Picker isolation is a single filter.** `AllPanes`/`PanesOf` pass
  `-f NotSidebarFilter` to `list-panes`, so sidebar panes never appear in picker
  rows, `list --query` text capture, or preview stacking — no `picker/`/`preview/`
  edits needed.
- **Sanitize every external string before rendering.** `sidebar.Sanitize` strips
  C0/C1/ESC/DEL from prompts, details, and paths (blocking OSC 52 clipboard and
  OSC 0/2 title injection) before display-width-aware truncation. Only
  tmux-generated ids are interpolated into hooks/`run-shell`, and the binary path
  is shell-quoted.
- **`[sidebar]` config is its own package, separate from `notify/`.** `config/`
  decodes only the `[sidebar]` table from twm.toml (so the Telegram token in
  `[telegram]` is never retained), with defaults `{enabled:true, width:32,
  refresh_ms:1000}`, clamps (width 20..80, refresh 500..10000), and precedence
  runtime `@twm_sidebar_enabled` > file > defaults. Path resolution duplicates
  `notify/config.go` deliberately to keep `notify/` untouched; a malformed file
  yields defaults + a generic `ErrConfig` that never echoes file contents.
- **PATH priming.** `run-shell` gives a minimal env, so `cli.Execute` prepends
  `/opt/homebrew/bin` and `/usr/local/bin` before any `fzf`/tmux exec.
- **Build-on-install.** `tmux-window-manager.tmux` rebuilds when the binary is
  missing or older than any `.go` file, and publishes the binary path as the
  `@twm_bin` tmux option so status-bar formats can call it.
- **Docs are an isolated static app.** `docs/` owns its Astro/Starlight npm
  dependencies and lockfile; it does not participate in the Go build or access
  tmux at build time. `SITE_URL` is optional and enables canonical URLs plus a
  sitemap. Cloudflare Pages uses root `docs`, build command `npm run build`, and
  output `dist`. Root `docs-*` Make targets are convenience wrappers. Generated
  `.astro`, `node_modules`, and `dist` content is ignored.

## Verifying parity

The original script lives in the dotfiles repo
(`scripts/tmux_window_manager.sh`). The picker keeps the same overall grouped
shape (`label`, `preview`, plain-window rows), but visible window rows now mirror
the tmux status-panel label plus bot/status instead of repeating `session:index`
or showing raw/model-enriched labels. Agent **status** is event-driven rather
than polled, so live badges intentionally differ (running/waiting text vs idle,
sourced from the DB).

## Tests

`go test ./...` — table-driven coverage of the process-tree walk + `NearestAgent`
ancestor walk, the transcript tail reader and parse helpers, hook payload
normalization (Claude + Codex), the `store` layer (upsert/conflict-merge,
`LiveByCwd` pid-liveness + reap, concurrency), the picker enricher + status glyphs,
`install-hooks` idempotent merge, directory lister, fzf option assembly, switch
command building, and PATH priming. The interactive popup/fzf
path and the end-to-end hook → DB → badge flow are verified manually in a real
tmux session (see the smoke tests in the PR).

Documentation verification uses `npm --prefix docs ci`,
`npm --prefix docs run build`, and `npm audit --prefix docs --omit=dev`.
`make docs-build` provides the clean-install production build shortcut. A build
with `SITE_URL` set additionally verifies canonical and sitemap generation.
