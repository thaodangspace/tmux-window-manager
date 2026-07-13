# AGENTS.md

Cached architecture notes for `tmux-window-manager`. Read this before changing
code; update it when you introduce new modules, commands, or design decisions.

## What this is

A self-contained Go port of a `tmux_window_manager.sh` dotfiles script: a fuzzy
tmux window switcher (`prefix + w`) shipped as a TPM plugin that builds its
binary on install. The port replaces the script's `jq` / `awk` / `fd` / `t2`
dependencies with native Go; the only external runtime deps are `tmux` and
`fzf`.

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
  hook.go      record an agent lifecycle event -> status DB (always exits 0)
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
  store.go       schema, Upsert/Delete, LiveByCwd (pid-liveness + lazy reap)
  alive.go       kill(pid,0) liveness (unix)
picker/     list row building + fzf invocation
  build.go     rows; Enricher iface; status -> glyph (running/waiting/idle)
  enrich.go    LiveEnricher: pane paths + status map -> badges (pure lookups)
  color.go     ANSI palette (Loader spinner, Waiting bell)
  fzf.go       fzf option assembly, ShellQuote, selection temp-file paths
dirs/       native Git-repo directory lister for Ctrl-N (replaces fd)
preview/    preview rendering (stacked panes; pane-names via process detection)
tmux-window-manager.tmux   TPM entry: build-on-install + bind key + publish @twm_bin
```

## Command surface

| Subcommand | Purpose |
|------------|---------|
| `run` / `popup` / `list` / `preview` / `label` / `open-editor` | the picker UI (ports of the original script modes) |
| `hook [event] [--agent] [--codex]` | record one agent lifecycle event into the status DB |
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
- **Popup → outer handoff via temp files.** `switch-client` from inside a popup
  is undone when the popup closes, so the popup writes the selection + fzf exit
  code to `$TMPDIR/tmux_wm_{sel,err}_<client>.txt` and the outer `run` acts after
  the popup closes. Client `/` is sanitized to `_` in the filename.
- **Ctrl-N directory suggestions are Git repos only.** The new-session picker
  still walks the configured roots (`currentDir`, `$HOME`, `~/code`, `~/go`, and
  `$HOME` top-level children), but emits only candidates with a direct `.git`
  entry. Manually typed paths are still accepted/created by `newSession`.
- **PATH priming.** `run-shell` gives a minimal env, so `cli.Execute` prepends
  `/opt/homebrew/bin` and `/usr/local/bin` before any `fzf`/editor exec.
- **Build-on-install.** `tmux-window-manager.tmux` rebuilds when the binary is
  missing or older than any `.go` file, and publishes the binary path as the
  `@twm_bin` tmux option so status-bar formats can call it.

## Verifying parity

The original script lives in the dotfiles repo
(`scripts/tmux_window_manager.sh`). The picker *layout* still mirrors it
(`label`, `preview`, plain-window rows), but agent **status** is now event-driven
rather than polled, so the live badges intentionally differ (running spinner vs
waiting bell vs idle, sourced from the DB).

## Tests

`go test ./...` — table-driven coverage of the process-tree walk + `NearestAgent`
ancestor walk, the transcript tail reader and parse helpers, hook payload
normalization (Claude + Codex), the `store` layer (upsert/conflict-merge,
`LiveByCwd` pid-liveness + reap, concurrency), the picker enricher + status
glyphs, `install-hooks` idempotent merge, directory lister, fzf option assembly,
switch command building, and PATH priming. The interactive popup/fzf path and the
end-to-end hook → DB → badge flow are verified manually in a real tmux session
(see the smoke tests in the PR).
