# Sidebar spike — FINDINGS (Phase 1)

Spec: `01_spec_persistent_agents_sidebar.md` · Plan: `01_impl_persistent_agents_sidebar.md`
(Phase 1, steps 1–7). Reproduce with `bash docs/plans/sidebar-spike/spike.sh <twm-bin>`.

- **Environment:** tmux 3.5a, Go 1.25.0, macOS (darwin/arm64). All tmux calls ran on
  an isolated server `tmux -L twm-spike -f /dev/null`, killed on exit. The user's
  live server was never touched.
- **tmux version evidence:** Homebrew `CHANGES` at
  `/opt/homebrew/Cellar/tmux/3.5a/CHANGES`, section "CHANGES FROM 3.1c TO 3.2":
  - "Add a **-f filter** argument to the list commands like choose-tree." →
    `list-panes -f` is available from **3.2** (the plugin's floor).
  - The same 3.2 section lists **`window-pane-changed`** among the hooks moved to
    window options, so it exists at ≥3.2 too.
- **Approximation gaps (marked below):** true mouse drag/click, `choose-tree`
  selection, `display-panes` focus, copy-mode/mouse-wheel input, an attached
  client, and tmux-resurrect were not exercised with a real tty; each is noted
  "scripted approximation; manual confirm pending".

---

## R1 — Hook coverage

Which hooks fire per lifecycle event (all candidates registered at index 90 on the
isolated server, command-driven):

| Event driven | Hooks that fired |
|---|---|
| `new-window` | after-new-window, window-layout-changed, window-linked |
| `new-session -d` | session-created, window-linked |
| `split-window` | after-split-window, window-layout-changed |
| `break-pane` | window-layout-changed, window-linked, window-pane-changed |
| `link-window` | window-linked |
| `move-window` | window-linked, window-unlinked |
| `choose-tree` select *(approx: `select-window`; manual confirm pending)* | after-select-window |
| `kill-pane` | after-kill-pane, window-layout-changed |
| pane process exit | pane-exited, window-layout-changed |
| `client-attached` / `client-resized` / `client-session-changed` / `session-window-changed` | **not observable without an attached tty** (approximation gap; wake expansion validated in WAKE) |

**Non-existent hook names on 3.5a (set-hook rejected them):** `after-break-pane`
and `after-move-window` do **not** exist. Break-pane is covered by
`window-layout-changed` + `window-linked` + `window-pane-changed`; move-window by
`window-linked` + `window-unlinked`. This corrects the plan's step-2 candidate list.

### pane_start_command marker — IMPORTANT correction to the plan

The plan/spec identify sidebar panes by `#{pane_start_command}` **ending in**
` sidebar render`. On tmux 3.5a this is **wrong**: tmux re-quotes multi-word start
commands. Passing `'<bin>' sidebar render` is stored as:

```
"'/…/twm' sidebar render"      # note the surrounding double quotes, ends with: render"
```

Even a fully bare `/tmp/twm sidebar render` is stored as `"/tmp/twm sidebar render"`.
Measured directly:

- `#{m:*sidebar render,#{pane_start_command}}`  → **0 (no match)** — suffix marker fails.
- `#{m:* sidebar render*,#{pane_start_command}}` → **1 (match)** — contains marker works.

**Decision:** the marker must be **CONTAINS ` sidebar render`**, not a suffix.
- Go (`IsSidebarStartCommand`, Phase 7): `strings.Contains(s, " sidebar render")`.
- tmux format (Phases 10/11): `#{m:* sidebar render*,#{pane_start_command}}`.

### `list-panes -f` (Phase 10 isolation) — works on 3.5a

```
IS-sidebar   -f '#{m:* sidebar render*,#{pane_start_command}}'          → sidebar pane only
NOT-sidebar  -f '#{?#{m:* sidebar render*,#{pane_start_command}},0,1}'  → real panes only
```

### tmux-resurrect

Installed, but **not executed in the spike**: resurrect operates on the *default*
tmux server, not `-L twm-spike`, so running it would risk the user's live tmux
(forbidden). Mitigation (already in the plan/spec): `ensure` dedupes panes by start
command, so a restored plain pane whose start command contains ` sidebar render` is
recognized and de-duplicated. **Manual confirm pending** on a scratch server.

---

## R2 — Layout-change frequency & re-dock correctness

- **Frequency:** one genuine layout change → **1** `window-layout-changed` firing
  (50 real resizes → 51 firings, ≈1.02/resize). A single `list-panes -a` (the
  no-op `ensure` proxy) takes **~10 ms**.
- **Coalescing:** driving resizes far faster than tmux redraws (≈243/s) collapsed
  to ~1 firing (~5/s reaching the hook). So the hook rate is bounded by tmux's
  redraw/dispatch, not by raw event rate.
- **Real fast mouse drag: approximation gap.** Each genuine per-column change fires
  one hook, and a fast drag can produce many changes/second, so the run-shell spawn
  rate *could* exceed ~10/s during an aggressive drag. Not reproducible without a
  real mouse. **Recommendation:** Phase 11 should include the tmux-only `if -F`
  geometry guard before `run-shell` as a precaution (see decision gate).
- **Re-dock under every layout:** after `select-layout {tiled,main-vertical,
  main-horizontal,even-horizontal}`, kill+recreate of the sidebar
  (`split-window -hbf -d -l 32`) placed it **full-height on the left**
  (`pane_left=0`, `pane_height==window_height`) in all four cases, and produced
  **0** follow-up `window-layout-changed` firings in the following second → the
  stateless re-dock does **not** loop (check-before-act ensure is safe).

---

## R3 — Focus

- **`window-pane-changed` context = the new active pane.** `select-pane` to the
  sidebar logged `window-pane-changed @0 %1` while the active pane was `%1` — the
  hook sees the newly-focused pane, so a tmux-only bounce can act on it directly.
  (`display-panes` and mouse click are approximation gaps: `display-panes` without
  a client fired nothing; manual confirm pending.)
- **`if -F … last-pane` with no last pane:** `last-pane` **errors** (`rc=1`,
  stderr `no last pane`). It aborts and would surface in the hook. Do **not** rely
  on `last-pane` alone.
- **`select-pane -R` fallback:** on a single pane it is a safe no-op (`rc=0`, no
  error). Since the sidebar is always leftmost, `-R` reliably returns focus to a
  real pane. **Recommendation:** bounce with `select-pane -R` (optionally try
  `last-pane` first, but the fallback must not depend on `last-pane`'s exit).
- **`select-pane -d` input blocking:** `send-keys` to a `-d` pane is **BLOCKED**
  (input-disabled is effective for programmatic keys). Typed keys / mouse wheel /
  copy-mode / display-panes need a live client — approximation gap, manual confirm
  pending, but input-disabled itself is confirmed.

---

## Wake

- **SIGUSR1 delivery via `#{P:…}` expansion: YES.** The exact form
  ```
  run-shell -b "kill -USR1 #{P:#{?#{m:* sidebar render*,#{pane_start_command}},#{pane_pid} ,}} 2>/dev/null; true"
  ```
  expanded to the sidebar pane's pid and the trap in the stand-in fired (woke=1).
  `#{P:…}` loops all panes and emits pids only for sidebars. (The plan's marker was
  updated to the contains form; otherwise the expansion is empty.)
  Firing of `session-window-changed` / `client-session-changed` needs an attached
  client (approximation gap); the expansion+delivery mechanism itself is validated.
- **Zoom/unzoom each fire `window-layout-changed` (+1), no `window-pane-changed`,
  no `window-resized`.** So zoom-driven visibility changes are already covered by
  the `window-layout-changed` ensure hook — no separate zoom wake hook is required.

---

## R4 — Runtime cost

- **Idle RSS per sidebar:** Go probe importing `store`, `Open()` + `Live()`, blocked
  on a signal → `ps -o rss=` = **12.67 MB** (probe lives in the scratchpad, not the
  repo). **Below the 15 MB gate**, but not by a wide margin: N sidebars ≈ N×12.7 MB
  (e.g. 10 windows ≈ 127 MB). Pure-Go `modernc.org/sqlite` is the main contributor.
- **`ps -axo pid=,ppid=,comm=` cost:** ~42 ms on the current table (~1080 procs);
  ~62 ms with +500 extra processes (1580 procs). Scales gently.
- **Visible render-tick CPU (proxy = ps + `list-panes -a` at 1 s):** **~5.6% of one
  core** for a single visible sidebar. Above the spec's ≲3% target; `ps` dominates.
  Only *visible* sidebars tick (typically one window per client), so the aggregate
  is ~5.6%, not N×. **Not a gate**, but flag for Phase 9: consider a cheaper process
  scan or skipping `ps` when `data_version` + pane set are unchanged to reach ≲3%.

---

## Decision gate outcome

| Gate | Measured | Outcome |
|---|---|---|
| RSS > 15 MB/sidebar → escalate (Option A′, tmux ≥3.3) | 12.67 MB | **Not tripped.** Stay on **Option A** (one persistent pane per window). No user escalation. |
| `list-panes -f` absent in 3.2 → Phase 10 Go-side filtering | `-f` present since 3.2; works on 3.5a | **Not tripped.** Phase 10 uses the tmux `-f` `NotSidebarFilter` (contains form). No Go fallback. |
| Layout firings > ~10/s during drags → Phase 11 `if -F` geometry guard | ~1/genuine change; bursts coalesce to ~5/s; real fast drag = approximation gap | **Soft recommendation:** include the tmux-only `if -F` geometry guard before `run-shell` in Phase 11 as a precaution. |

**Option A vs A′:** **Option A** (RSS under budget).

---

## Recommended final hook list for Phase 11 (index 90)

Marker used everywhere below: `#{m:* sidebar render*,#{pane_start_command}}`.

1. **Ensure triggers** — `run-shell -b "<quoted-bin> sidebar ensure -t #{window_id}"`:
   - `after-new-window`
   - `session-created`
   - `after-split-window`
   - `window-layout-changed`  *(also covers zoom/unzoom and break-pane)*
   - `window-linked`  *(covers link-window and the destination of break/move)*
   - `window-unlinked`  *(covers move-window source)*
   - `pane-exited`
   - `after-kill-pane`
   - `window-resized` and `client-resized`  *(width recompute; client-resized needs a client)*
   - *Optional per the gate:* precede `run-shell` with a tmux-only `if -F` geometry
     guard so a drag over already-correct geometry does not spawn a process.
2. **Focus bounce** — tmux-only, no process spawn:
   - `window-pane-changed` →
     `if -F '#{m:* sidebar render*,#{pane_start_command}}' 'select-pane -R'`
     (context is the new active pane; `select-pane -R` is the reliable bounce;
     `last-pane` errors when there is no last pane, so do not depend on it).
3. **Visibility wake** — `run-shell -b "kill -USR1 #{P:#{?#{m:* sidebar render*,#{pane_start_command}},#{pane_pid} ,}} 2>/dev/null; true"`:
   - `session-window-changed`
   - `client-session-changed`
   - `after-select-window`
   *(zoom/unzoom need no wake hook — they already fire `window-layout-changed`.)*

**Do not use** `after-break-pane` or `after-move-window` — they do not exist on
tmux 3.5a (set-hook rejects them).

---

## Pane-filter form for Phase 10

- **NotSidebarFilter** (keep real panes): `#{?#{m:* sidebar render*,#{pane_start_command}},0,1}`
- IS-sidebar: `#{m:* sidebar render*,#{pane_start_command}}`
- Go `IsSidebarStartCommand`: `strings.Contains(s, " sidebar render")` — **contains,
  not suffix** (tmux re-quotes `pane_start_command` so it ends with a `"`).

---

## Unknowns / manual-confirm pending

- Real mouse drag firing rate (>10/s?) and mouse click / `display-panes` focus.
- `choose-tree` interactive selection hooks (approximated with `select-window`).
- `client-attached` / `client-resized` / `client-session-changed` /
  `session-window-changed` live firing (need an attached tty).
- Mouse-wheel / copy-mode entry into a `select-pane -d` pane.
- tmux-resurrect save/restore of a sidebar pane and the restored start command.
- Whether the ~5.6% visible-tick CPU can be brought under 3% (Phase 9 tuning).
