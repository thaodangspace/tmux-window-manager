# AGENTS.md

Cached architecture notes for `tmux-window-manager`. Read this before changing
code; update it when you introduce new modules, commands, or design decisions.

## What this is

A self-contained Go port of a `tmux_window_manager.sh` dotfiles script: a fuzzy
tmux window switcher (`prefix + w`) shipped as a TPM plugin that builds its
binary on install. The port replaces the script's `jq` / `awk` / `fd` / `t2`
dependencies with native Go; the only external command dependencies are `tmux`
and `fzf`. Optional Telegram notifications use the Bot API directly over HTTPS.

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
  openeditor.go Zed/Typora launch
  hook.go      record an agent lifecycle event -> status DB; optional Telegram
               side effect for Claude Notification/Stop (always exits 0)
  serveattach.go hidden short-lived loopback tmux attach helper
  installhooks.go  merge hooks into ~/.claude/settings.json + Codex snippet
  status.go    debug dump of the status rows
tmuxcli/    typed wrappers over the tmux CLI (one ps/tmux call shape per func)
agents/     agent detection + hook payload normalization
  proc.go        process-subtree walk -> agent names (port of the awk);
                 NearestAgent() ancestor walk for hook pid resolution
  transcript.go  Claude .jsonl tail reader (model/latest) + parse helpers
  hookpayload.go normalize Claude (stdin) / Codex (notify) payloads -> status
store/      SQLite status persistence (the event-driven status source)
  path.go        canonical DB path ($TWM_DB_PATH / $XDG_STATE_HOME / ~/.local/state)
  store.go       schema, Upsert/Get/Delete, LiveByCwd (pid-liveness + lazy reap)
  alive.go       kill(pid,0) liveness (unix)
notify/     optional outbound lifecycle notifications
  config.go     ~/.config/twm.toml + environment loading and precedence
  telegram.go   safe message composition + Telegram sendMessage client
attachlink/ short-lived, loopback-only single-use tmux focus capability
  server.go     inert GET, token-gated POST, security headers, expiry
  launcher_unix.go detached helper startup/readiness/cleanup lifecycle
picker/     list row building + fzf invocation
  build.go     rows; Enricher iface; visible status-panel-label/bot/status format
  enrich.go    LiveEnricher: pane paths + status map -> badges (pure lookups)
  color.go     ANSI palette (robot icon + running/waiting status text)
  fzf.go       fzf option assembly, ShellQuote, selection temp-file paths
dirs/       native Git-repo directory lister for Ctrl-N (replaces fd)
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
| `run` / `popup` / `list` / `preview` / `label` / `open-editor` | the picker UI (ports of the original script modes) |
| `hook [event] [--agent] [--codex]` | record one lifecycle event; optionally notify Telegram for Claude Notification/Stop |
| `serve-attach` | hidden one-shot helper for a loopback tmux focus link |
| `install-hooks [--claude] [--codex] [--dry-run]` | wire the hooks into Claude/Codex config |
| `status [--all]` | debug dump of the status rows |

The binary re-invokes itself via `os.Executable()` (the script used `$BASH_SOURCE`).

## Key design decisions

- **Event-driven status, not polling.** Agents push lifecycle events (start /
  prompt / notification / stop / end) by invoking `twm hook <event>` from Claude
  Code hooks (stdin JSON) and the Codex `notify` program (argv JSON). Each firing
  upserts one row into a SQLite DB keyed by `(agent, session_id)`. The picker
  reads the DB — there is **no** `capture-pane` busy regex and **no** transcript
  JSON cache anymore (both removed). The payoff is a real **waiting-on-user**
  state (`🔔`) that pane-scraping could never detect reliably.
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
- **Telegram is an optional post-DB side effect.** Credentials are loaded from
  `[telegram]` in `~/.config/twm.toml` (or `$XDG_CONFIG_HOME/twm.toml`), with
  non-empty `$TWM_TELEGRAM_BOT_TOKEN` / `$TWM_TELEGRAM_CHAT_ID` values overriding
  the corresponding file fields. `Notification` and `Stop` hooks synchronously
  call Bot API `sendMessage` after a successful status upsert. The client has a
  2s timeout, no retry/outbox/daemon, MarkdownV2 messages (plain summary line,
  bold detail labels) capped below Telegram's limit, and token-safe error
  categories logged only with `$TWM_HOOK_DEBUG`.
  Every message includes a status icon, project basename, session ID, and the
  first user prompt; Notification also includes its detail, while Stop excludes
  assistant response text. Full paths, other transcript content, PIDs, model
  names, and credentials are not sent. Telegram failures never alter DB state
  or the hook's zero exit status; Codex/Pi do not send Telegram messages.
- **Telegram tmux attach links are tertiary best effort.** For an eligible Claude
  notification, a live `TMUX_PANE` can start one detached `serve-attach` helper
  after the DB write and Telegram configuration succeed. It binds only to
  `127.0.0.1`, uses a 128-bit in-memory token, keeps GET inert, switches only
  the most recently active existing tmux client on exact POST, and expires after
  15 minutes or one successful switch. The token/target/client/path never enter
  Telegram text, SQLite, or debug logs. Link creation and cleanup failures omit
  only the link; they never fail the hook. This is for Telegram Desktop and a
  browser on the same host; it does not launch Ghostty, create SSH sessions, or
  provide a persistent daemon. Pi remains deferred.
- **Popup → outer handoff via temp files.** `switch-client` from inside a popup
  is undone when the popup closes, so the popup writes the selection + fzf exit
  code to `$TMPDIR/tmux_wm_{sel,err}_<client>.txt` and the outer `run` acts after
  the popup closes. Client `/` is sanitized to `_` in the filename.
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
- **Ctrl-N directory suggestions are Git repos only.** The new-session picker
  still walks the configured roots (`currentDir`, `$HOME`, `~/code`, `~/go`, and
  `$HOME` top-level children), but emits only candidates with a direct `.git`
  entry. Manually typed paths are still accepted/created by `newSession`.
- **PATH priming.** `run-shell` gives a minimal env, so `cli.Execute` prepends
  `/opt/homebrew/bin` and `/usr/local/bin` before any `fzf`/editor exec.
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
`LiveByCwd` pid-liveness + reap, concurrency), Telegram TOML/environment config precedence, composition/HTTP transport
security and failures, hook → DB → optional notification ordering and
filtering, the picker enricher + status glyphs, `install-hooks` idempotent merge,
directory lister, fzf option assembly, switch command building, and PATH
priming. The interactive popup/fzf path and the end-to-end hook → DB → badge flow
are verified manually in a real tmux session (see the smoke tests in the PR).

Documentation verification uses `npm --prefix docs ci`,
`npm --prefix docs run build`, and `npm audit --prefix docs --omit=dev`.
`make docs-build` provides the clean-install production build shortcut. A build
with `SITE_URL` set additionally verifies canonical and sitemap generation.
