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
  killsession.go  internal fzf Ctrl-X action: kill selected window/session + reload
  popup.go     inside the popup: fzf, writes selection temp files
  list.go      emit fzf rows (live enricher reads the status DB)
  preview.go   fzf preview
  label.go     status-bar agent name (process detection)
  hook.go      deprecated no-op (exits 0) for hooks older releases installed
  jump.go      switch the most recent client to a pane
  notifywait.go  macOS sender: detached alerter waiter -> jump on click
  watch.go     pane watcher daemon: capture agent panes -> status DB + local model judge -> backends
  backends.go  backend fan-out (deliver), per-backend filters, debugf / TWM_DEBUG
  uninstallhooks.go  remove twm hooks from Claude settings.json; point at the Codex notify line
  status.go    debug dump of the status rows
tmuxcli/    typed wrappers over the tmux CLI (one ps/tmux call shape per func)
agents/     coding-agent process detection
  registry.go    Kind{ID,Display} registry (claude, codex, pi, gemini, opencode,
                 cursor-agent, aider, amp, goose, qwen, crush, droid, copilot)
                 + IsAgent/DisplayName
  proc.go        process-subtree walk -> agent names (port of the awk);
                 interpreter scripts (node/bun/deno/python*) resolved to the
                 script basename; Agents()/AgentGroups() outermost-agent-per-chain
store/      SQLite status persistence (written only by the watcher)
  path.go        canonical DB path ($TWM_DB_PATH / $XDG_STATE_HOME / ~/.local/state)
  store.go       schema, Upsert/Get/Delete, LiveByCwd (pid-liveness + lazy reap)
  alive.go       kill(pid,0) liveness (unix)
notify/     optional notification backends + model judge
  config.go    twm.toml: [telegram] + [macos] sections, shared option keys
  telegram.go  MarkdownV2 Compose + Bot API delivery
  macos.go     ComposePlain + alerter argv / click-result parsing
  poller.go    [poller] watcher config + Judge (LM Studio chat/completions, json_schema verdict)
picker/     list row building + fzf invocation
  build.go     rows; Enricher iface; visible status-panel-label/bot/status format
  enrich.go    LiveEnricher: pane paths + status map -> badges (pure lookups)
  color.go     ANSI palette (robot icon + idle/working/waiting/error status text)
  fzf.go       fzf option assembly, ShellQuote, selection temp-file paths
dirs/       native Git-repo directory lister for Ctrl-N (replaces fd)
  list.go        GitRoot(path): nearest ancestor with .git, stops before $HOME
preview/    preview rendering (stacked panes; pane-names via process detection)
docs/       isolated Astro/Starlight static documentation site
  src/content/docs/ user guides and reference pages
  src/assets/       docs-owned copies of picker screenshots
  public/_headers   Cloudflare Pages response headers
tmux-window-manager.tmux   TPM entry: build-on-install + bind key + publish @twm_bin + (re)start watcher
```

## Command surface

| Subcommand | Purpose |
|------------|---------|
| `run` / `popup` / `list` / `preview` / `label` | the picker UI (ports of the original script modes) |
| `toggle-agents [client]` | flip the picker's per-client agents-only filter (Ctrl-A) |
| `kill-session [client] [target]` | internal Ctrl-X action; kill selected window/session and reload without exiting fzf |
| `hook ...` | hidden, deprecated no-op (exits 0) so hooks from older releases don't error |
| `jump [--socket] <pane>` | focus a pane in the most recent client |
| `notify-wait ...` | detached macOS notification waiter; jumps on click |
| `watch [--detach] [--replace] [--once] [--socket S]` | hidden pane watcher daemon: agent status + notifications |
| `uninstall-hooks [--dry-run]` | remove twm hooks from Claude settings; print the Codex notify line to delete |
| `status [--all]` | debug dump of the status rows |

The binary re-invokes itself via `os.Executable()` (the script used `$BASH_SOURCE`).

## Key design decisions

- **Status from the panes, not hooks.** The hidden `watch` daemon is the only
  writer of agent status and the only source of notifications. Claude/Codex
  hooks, transcript reading, and the hook installer were removed: hooks covered
  only some agents and needed per-agent setup. `hook` survives as a no-op that
  exits 0 (without reading stdin, which Codex may never close) so hooks left by
  older releases don't error; `uninstall-hooks` removes the Claude groups (any
  hook command containing `tmux-window-manager` and ` hook`) and only *reports*
  the Codex `notify` line, since rewriting TOML would lose comments/layout.
- **Watcher loop.** The `.tmux` entry always runs `watch --detach --replace
  --socket <socket_path>`, so a rebuild restarts it; it exits when the tmux
  server is gone. One instance via an exclusive `flock` on `watch.pid` next to
  the DB (not `$TMPDIR`, which differs between environments); `--replace`
  SIGTERMs the pid recorded under the lock. On start it deletes every status
  row and rebuilds from the panes. Every `scan_seconds` (5) it lists agent
  panes (`AllPanes` + `AgentGroups`, outermost agent pid), captures the last
  `lines` (60), and hashes the screen with digits stripped so timers/token
  counters are not change. Changed → `working` (and `busySince` is set when
  the pane turns working). Unchanged for `settle_seconds` (6) → judged once;
  a screen judged `working` that stays unchanged is re-judged every
  `interval_seconds` (120). Rows are keyed `(agent, "pane:%N")`, carry the
  agent pid (the picker joins on it), and are written only when status,
  detail, model, or pid changed; rows of panes that no longer run an agent are
  deleted. `[poller]` is read once at start (restart to apply); backend
  sections are re-read per notification.
- **Optional model judge (`[poller] enabled = true`).** `notify.Judge` is an
  OpenAI-compatible `chat/completions` call (LM Studio, default
  `google/gemma-4-12b` at `localhost:1234/v1`) with a `json_schema` response:
  `working|waiting|completed|error|idle`, a one-sentence summary, and the
  model name if printed on screen. The verdict becomes the row (`completed`
  and `idle` → `idle`), summary → detail, model → picker model label.
  Redirects are refused so screen text only reaches the endpoint. A judge
  failure sets a settled pane to `idle` and retries after a 30s backoff.
  Without the judge: changed = `working`, settled = `idle`, no notifications.
  `watch --once` judges every agent pane and prints the verdicts (no writes,
  no delivery; requires the judge).
- **Notifications follow verdicts.** `waiting` / `completed` / `error` notify
  at most once per settled screen (`notifiedHash`); screens present on the
  first scan are seeded as notified, so startup is silent. `error` goes out
  as "needs input" with a `⚠️` detail prefix. `min_turn_seconds` applies to
  completed events using the busy duration (first change → last change);
  `skip_when_focused` uses `tmuxcli.LookupPane` (visible pane of a client with
  input in the last 2 minutes); `include_response` attaches the summary to
  completed events. Telegram "finished" messages go out silently.
- **Backends fan out with independent filters.** `notifiersFromConfig`
  returns every enabled backend (Telegram, macOS); `deliver()` applies each
  backend's own Options. Delivery errors are reduced to categories before
  `debugf` logs them (`TWM_DEBUG=1` → `$TMPDIR/twm_debug.log`) because raw
  net/http errors can carry the token-bearing URL.
- **macOS notifications jump on click.** A `[macos] enabled = true` section
  posts the same events through `alerter` (osascript cannot act on a click;
  terminal-notifier 2.0's `-execute` never fires on current macOS). alerter
  blocks until the notification is clicked/dismissed/timed out and prints
  JSON, so the watcher spawns a detached (`Setsid`, stdio on /dev/null)
  `twm notify-wait` that runs alerter and, on `contentsClicked`, jumps to the
  agent pane via the watcher's tmux socket (`tmuxcli.Socket` adds `-S`) and
  `open -b terminal_bundle_id`. One waiter per pane: the pid lives in
  `$TMPDIR/twm_notify_<group>.pid`, and a new event kills the previous
  waiter's process group (only if that pid still runs `notify-wait`).
  alerter values use `--opt=value` so text starting with `-` is safe.
- **Status source of truth: `store`.** DB at
  `~/.local/state/tmux-window-manager/agents.db` (override `$TWM_DB_PATH`, honors
  `$XDG_STATE_HOME`). WAL + `busy_timeout` so picker reads don't block the
  watcher. Pure-Go `modernc.org/sqlite` keeps the build cgo-free (but bumps the Go
  floor to 1.25). Rows carry the agent **pid**; reads hide and lazily reap rows
  whose process is dead. The schema still has v2 columns (`turn_prompt`,
  `turn_started_at`, `notified_at`) from the hook era; the watcher leaves them
  empty. Agent presence/name in the picker comes from process detection; the
  DB only adds status and model.
- **Popup → outer handoff via temp files.** `switch-client` from inside a popup
  is undone when the popup closes, so the popup writes the selection + fzf exit
  code to `$TMPDIR/tmux_wm_{sel,err}_<client>.txt` and the outer `run` acts after
  the popup closes. Enter switches targets, Ctrl-N creates a session, and Ctrl-X
  kills only the selected window row or the session of a selected header. Client `/` is sanitized
  to `_` in the filename.
- **Ctrl-X never detaches the client.** Window rows use `kill-window`; the last
  window in a session routes through `kill-session` so the client can switch
  first; the last remaining session is refused. Session headers use
  `kill-session`. With tmux's default
  `detach-on-destroy on`, killing the attached session would drop the user out
  of tmux, so when the target is the client's current session `run` prepends a
  `switch-client` to the client's last session (else the first other session)
  in the same tmux invocation. The last remaining session is refused. Session
  targets are `=`-prefixed for exact (not prefix) matching. Window counts use
  `display-message -t =session:index` (a bare `=session` has no format context
  on tmux 3.5a and returns an empty `session_windows`). Ctrl-X runs via an
  fzf execute-silent binding followed by reload, keeping the picker open.
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
  The visible row mirrors the status bar data as
  `🤖 basename(pane_current_path)(git_branch)/label`, where the robot and branch
  are optional and `label` is the detected agent name or `pane_current_command`
  fallback, followed by optional ` - status`.
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
- **PATH priming.** `run-shell` gives a minimal env, so `cli.Execute` prepends
  `/opt/homebrew/bin` and `/usr/local/bin` before any `fzf`/tmux exec.
- **Build-on-install.** `tmux-window-manager.tmux` rebuilds when the binary is
  missing or older than any `.go` file, and publishes the binary path as the
  `@twm_bin` tmux option so status-bar formats can call it. It also removes
  legacy sidebar panes, index-90 hooks, and options left by older releases.
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
or showing raw/model-enriched labels. Agent **status** comes from the watcher's
screen-change detection (plus the optional model judge) rather than the
script's busy regex, so live badges show `idle`/`working`/`waiting`/`error`
sourced from the DB.

## Tests

`go test ./...` — table-driven coverage of the process-tree walk, interpreter
script resolution and `AgentGroups`, the agent registry, the `store` layer
(upsert/conflict-merge, pid-liveness + reap, concurrency), the watcher tick
(change → working, settle → judge, re-judge interval, judge backoff, silent
startup, once-per-screen notifications, row reaping) with injected seams, the
judge request/response parsing and `[poller]` defaults, `uninstall-hooks`
removal and Codex line reporting, Telegram/macOS composition and config, the
notify-wait waiter, the picker enricher + status glyphs, fzf option assembly,
switch command building, and PATH priming. The interactive popup/fzf path and
the end-to-end pane → watcher → DB → badge flow (and LM Studio verdicts via
`watch --once`) are verified manually in a real tmux session.

Documentation verification uses `npm --prefix docs ci`,
`npm --prefix docs run build`, and `npm audit --prefix docs --omit=dev`.
`make docs-build` provides the clean-install production build shortcut. A build
with `SITE_URL` set additionally verifies canonical and sitemap generation.
