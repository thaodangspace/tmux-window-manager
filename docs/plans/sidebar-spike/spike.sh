#!/usr/bin/env bash
#
# spike.sh — Phase 1 spike for the persistent agents sidebar.
#
# Validates tmux/runtime risks R1–R4 + the wake path on an ISOLATED tmux server.
# It never touches the user's live tmux: every tmux invocation targets a private
# socket (-L twm-spike) started with an empty config (-f /dev/null), and the
# server is killed on exit via a trap.
#
# Re-runnable: any prior twm-spike server is killed first, and all scratch state
# lives under a fresh mktemp dir that is removed on exit.
#
# Usage:  bash docs/plans/sidebar-spike/spike.sh [path-to-twm-binary]
#
# The optional argument is a real `twm` binary used only to record the exact
# pane_start_command tmux stores for `'<bin>' sidebar render`. If omitted, a
# stand-in path is used (the `sidebar render` subcommand does not exist until a
# later phase, so the real binary would exit immediately anyway — only the
# stored start-command string matters here).
#
# The Go RSS probe (R4) is NOT built here; it lives in the scratchpad (see
# FINDINGS.md R4) so nothing extra is committed to the repo.

set -u

SOCK="twm-spike"
TMUX="tmux -L ${SOCK} -f /dev/null"
BIN="${1:-/opt/twm/tmux-window-manager}"

# Long-lived stand-in sidebar whose start command CONTAINS " sidebar render"
# (extra argv words keep the process alive while carrying the marker suffix).
SIDEBAR_STANDIN="sh -c 'trap \"\" INT; exec sleep 86400' x sidebar render"
# Long-lived plain (non-sidebar) stand-in.
PLAIN_STANDIN="sh -c 'trap \"\" INT; exec sleep 86400'"

WORK="$(mktemp -d "${TMPDIR:-/tmp}/twm-spike.XXXXXX")"
LOG="${WORK}/hooks.log"
: >"${LOG}"

cleanup() {
	tmux -L "${SOCK}" kill-server 2>/dev/null
	rm -rf "${WORK}"
}
trap cleanup EXIT INT TERM

say()  { printf '\n=== %s ===\n' "$*"; }
note() { printf '  %s\n' "$*"; }

# Count log lines for a given hook name (grep -c always prints a number).
logcount() { grep -c "^$1 " "${LOG}" 2>/dev/null; }

# List of candidate hooks (some names may not exist on this tmux; rejects noted).
CANDIDATES="after-new-window session-created after-split-window window-linked \
after-break-pane pane-exited after-kill-pane window-layout-changed \
window-resized client-resized session-window-changed client-session-changed \
after-select-window client-attached window-pane-changed window-unlinked \
after-move-window"

# (Re)register every candidate hook at fixed index 90 on the isolated server.
# Each firing appends "<hook> <window_id> <pane_id>" to $LOG.
register_hooks() {
	local h
	for h in ${CANDIDATES}; do
		${TMUX} set-hook -g "${h}[90]" \
			"run-shell -b \"echo ${h} #{window_id} #{pane_id} >> ${LOG}\"" 2>/dev/null \
			|| note "set-hook ${h} REJECTED (no such hook on $(tmux -V))"
	done
}

# Snapshot current total log size, run "$@", settle, then print which hooks fired.
fired_since() {
	local before after
	before="$(wc -l <"${LOG}")"
	"$@"
	sleep 0.4
	after="$(wc -l <"${LOG}")"
	if [ "${after}" -gt "${before}" ]; then
		sed -n "$((before + 1)),${after}p" "${LOG}" | awk '{print $1}' | sort -u | sed 's/^/    fired: /'
	else
		echo "    fired: (none)"
	fi
}

# --- start clean isolated server ------------------------------------------
tmux -L "${SOCK}" kill-server 2>/dev/null
${TMUX} new-session -d -s main -x 200 -y 50
${TMUX} set-option -g remain-on-exit off
register_hooks

########################################################################
say "ENV"
note "tmux: $(tmux -V)"
note "go:   $(go version 2>/dev/null || echo 'go not found')"
note "isolated socket: ${SOCK}   scratch: ${WORK}"
note "bin arg for start-command test: ${BIN}"

########################################################################
say "R1 — which hooks fire per lifecycle event"

# Give the main window a stable sidebar stand-in to work against later.
${TMUX} split-window -hbf -d -l 32 -t main "${SIDEBAR_STANDIN}" >/dev/null 2>&1

note "event: new-window"
fired_since ${TMUX} new-window -d

note "event: new-session -d"
fired_since ${TMUX} new-session -d -s extra -x 200 -y 50

note "event: split-window"
fired_since ${TMUX} split-window -d -t extra "${PLAIN_STANDIN}"

note "event: break-pane"
fired_since ${TMUX} break-pane -d -s extra

note "event: link-window"
fired_since ${TMUX} link-window -d -s extra:1 -t main:20 2>/dev/null

note "event: move-window"
fired_since ${TMUX} move-window -d -s main:20 -t main:21 2>/dev/null

note "event: choose-tree select (scripted approximation: switch-client + select-window; manual confirm pending)"
fired_since ${TMUX} switch-client -t extra
fired_since ${TMUX} select-window -t main:0

note "event: kill-pane (a plain pane)"
KP=$(${TMUX} split-window -d -P -F '#{pane_id}' -t main "${PLAIN_STANDIN}")
fired_since ${TMUX} kill-pane -t "${KP}"

note "event: pane process exit (short-lived process ends on its own)"
${TMUX} split-window -d -t main "sh -c 'sleep 0.5'" >/dev/null 2>&1
fired_since sleep 1.0

note "(client-attached / client-resized / client-session-changed / session-window-changed"
note " require a client with a real tty; command-line driving here does not attach one."
note " Their run-shell wake expansion is validated directly in the WAKE section below;"
note " live firing = scripted approximation, manual confirm pending.)"

########################################################################
say "R1 — exact pane_start_command tmux stores"
${TMUX} set-option -g remain-on-exit on
PS=$(${TMUX} split-window -hbf -d -l 32 -P -F '#{pane_id}' -t main "'${BIN}' sidebar render")
sleep 0.2
note "passed:  '${BIN}' sidebar render"
note "stored:  $(${TMUX} display-message -p -t "${PS}" '#{pane_start_command}')"
note "m suffix  #{m:*sidebar render,...}  = $(${TMUX} display-message -p -t "${PS}" '#{m:*sidebar render,#{pane_start_command}}')  (0 = NO match)"
note "m contains #{m:* sidebar render*,...} = $(${TMUX} display-message -p -t "${PS}" '#{m:* sidebar render*,#{pane_start_command}}')  (1 = match)"
${TMUX} kill-pane -t "${PS}" 2>/dev/null
${TMUX} set-option -g remain-on-exit off

########################################################################
say "R1 — list-panes -f filter (Phase 10 isolation) on $(tmux -V)"
${TMUX} kill-server 2>/dev/null
${TMUX} new-session -d -s main -x 200 -y 50
${TMUX} split-window -hbf -d -l 32 -t main "${SIDEBAR_STANDIN}" >/dev/null 2>&1
note "all panes:"
${TMUX} list-panes -t main -F '    #{pane_id} [#{pane_start_command}]'
note "IS-sidebar  -f '#{m:* sidebar render*,#{pane_start_command}}':"
${TMUX} list-panes -t main -f '#{m:* sidebar render*,#{pane_start_command}}' -F '    #{pane_id}'
note "NOT-sidebar -f '#{?#{m:* sidebar render*,#{pane_start_command}},0,1}' (NotSidebarFilter):"
${TMUX} list-panes -t main -f '#{?#{m:* sidebar render*,#{pane_start_command}},0,1}' -F '    #{pane_id}'

########################################################################
say "R1 — tmux-resurrect (installed)"
note "NOT executed in-spike: tmux-resurrect operates on the DEFAULT server, not"
note "-L twm-spike, so running it would risk the user's live tmux (forbidden)."
note "Mitigation reasoning recorded in FINDINGS.md; manual confirm pending."

########################################################################
say "R2 — window-layout-changed frequency"
${TMUX} kill-server 2>/dev/null
${TMUX} new-session -d -s main -x 200 -y 50
register_hooks
${TMUX} split-window -hbf -d -l 32 -t main "${SIDEBAR_STANDIN}" >/dev/null 2>&1
${TMUX} split-window -d -t main "${PLAIN_STANDIN}" >/dev/null 2>&1
: >"${LOG}"
note "driving 50x resize-pane -L 1 (scripted approximation of one mouse drag; manual confirm pending)"
for i in $(seq 1 50); do ${TMUX} resize-pane -t main -L 1 >/dev/null 2>&1; done
sleep 0.6
LC=$(logcount window-layout-changed)
note "window-layout-changed firings for 50 resizes = ${LC}  (~$(awk "BEGIN{printf \"%.2f\", ${LC}/50}") per resize)"
note "no-op ensure proxy (single list-panes -a) timing:"
perl -MTime::HiRes=time -e '
  my $t0=time; system("tmux -L '"${SOCK}"' -f /dev/null list-panes -a -F \"#{pane_id}\" >/dev/null 2>&1");
  printf "    one list-panes -a took %.1f ms\n", (time-$t0)*1000;'

say "R2 — re-dock correctness under layouts (kill + recreate)"
for layout in tiled main-vertical main-horizontal even-horizontal; do
	${TMUX} kill-server 2>/dev/null
	${TMUX} new-session -d -s main -x 200 -y 50
	register_hooks
	# two real panes + one (stale) sidebar, then force a layout
	${TMUX} split-window -d -t main "${PLAIN_STANDIN}" >/dev/null 2>&1
	SB=$(${TMUX} split-window -hbf -d -l 32 -P -F '#{pane_id}' -t main "${SIDEBAR_STANDIN}")
	${TMUX} select-layout -t main "${layout}" >/dev/null 2>&1
	# kill + recreate the sidebar (stateless re-dock)
	${TMUX} kill-pane -t "${SB}" 2>/dev/null
	NB=$(${TMUX} split-window -hbf -d -l 32 -P -F '#{pane_id}' -t main "${SIDEBAR_STANDIN}")
	sleep 0.5
	read -r LEFT TOP PH WH <<<"$(${TMUX} display-message -p -t "${NB}" '#{pane_left} #{pane_top} #{pane_height} #{window_height}')"
	FULL="no"; [ "${LEFT}" = "0" ] && [ "$((PH + 1))" -ge "${WH}" ] && FULL="yes"
	# "follow-up" = firings AFTER the re-dock settles (a hook loop would keep firing).
	: >"${LOG}"
	sleep 1.0
	SETTLE=$(logcount window-layout-changed)
	note "${layout}: sidebar left=${LEFT} top=${TOP} height=${PH}/${WH} full-height-left=${FULL}; post-redock firings in 1s=${SETTLE} (no-loop: $([ "${SETTLE}" -le 2 ] && echo yes || echo NO))"
done

########################################################################
say "R3 — focus: window-pane-changed context + bounce"
${TMUX} kill-server 2>/dev/null
${TMUX} new-session -d -s main -x 200 -y 50
register_hooks
SB=$(${TMUX} split-window -hbf -d -l 32 -P -F '#{pane_id}' -t main "${SIDEBAR_STANDIN}")
${TMUX} select-pane -d -t "${SB}" 2>/dev/null
REAL=$(${TMUX} list-panes -t main -f '#{?#{m:* sidebar render*,#{pane_start_command}},0,1}' -F '#{pane_id}' | head -1)
: >"${LOG}"
note "select-pane -> sidebar (${SB}); does window-pane-changed carry the new active pane?"
${TMUX} select-pane -t "${SB}" 2>/dev/null
sleep 0.4
note "    log: $(grep '^window-pane-changed ' "${LOG}" | tail -1)"
note "    active pane now: $(${TMUX} display-message -p -t main '#{pane_id}')"
note "display-panes (scripted approximation of the popup; manual confirm pending):"
: >"${LOG}"
${TMUX} display-panes -d 1 -t main >/dev/null 2>&1
sleep 0.4
note "    window-pane-changed after display-panes: $(logcount window-pane-changed)"

say "R3 — if -F last-pane with no last pane (does the error abort?)"
${TMUX} kill-server 2>/dev/null
${TMUX} new-session -d -s solo -x 200 -y 50   # single pane, no last-pane exists
OUT=$(${TMUX} if-shell -F '1' 'last-pane' 2>&1); RC=$?
note "if -F '1' 'last-pane' on a single-pane window: rc=${RC} out=[${OUT}]"
note "select-pane -R fallback on single pane:"
OUT=$(${TMUX} select-pane -R -t solo 2>&1); RC=$?
note "    rc=${RC} out=[${OUT}]"

say "R3 — select-pane -d input blocking"
${TMUX} kill-server 2>/dev/null
${TMUX} new-session -d -s main -x 200 -y 50
CAP="${WORK}/typed.txt"; : >"${CAP}"
# a pane that appends every line it receives to a file
SBK=$(${TMUX} split-window -hbf -d -l 40 -P -F '#{pane_id}' -t main "sh -c 'cat >> ${CAP}' x sidebar render")
${TMUX} select-pane -d -t "${SBK}" 2>/dev/null
${TMUX} send-keys -t "${SBK}" 'HELLO' Enter 2>/dev/null
sleep 0.4
if grep -q HELLO "${CAP}"; then
	note "send-keys to a -d pane: DELIVERED (send-keys bypasses input-disabled)"
else
	note "send-keys to a -d pane: BLOCKED (nothing received)"
fi
note "typed keys / mouse wheel / copy-mode / display-panes on a -d pane: require a"
note "live client; scripted approximation via send-keys above, manual confirm pending."

########################################################################
say "WAKE — kill -USR1 to sidebar pids via run-shell -b"
${TMUX} kill-server 2>/dev/null
${TMUX} new-session -d -s main -x 200 -y 50
register_hooks
WOKE="${WORK}/woke.txt"; : >"${WOKE}"
# sidebar stand-in that records SIGUSR1 delivery
SBW=$(${TMUX} split-window -hbf -d -l 32 -P -F '#{pane_id}' -t main \
	"sh -c 'trap \"echo woke >> ${WOKE}\" USR1; while :; do sleep 1; done' x sidebar render")
${TMUX} split-window -d -t main "${PLAIN_STANDIN}" >/dev/null 2>&1
sleep 0.3
note "expanded target pids: [$(${TMUX} display-message -p -t main '#{P:#{?#{m:* sidebar render*,#{pane_start_command}},#{pane_pid} ,}}')]"
${TMUX} run-shell -b "kill -USR1 #{P:#{?#{m:* sidebar render*,#{pane_start_command}},#{pane_pid} ,}} 2>/dev/null; true"
sleep 0.7
if grep -q woke "${WOKE}"; then
	note "SIGUSR1 delivered to the sidebar process via #{P:...} expansion: YES"
else
	note "SIGUSR1 delivery: NO"
fi

say "WAKE — does zoom/unzoom fire window-layout-changed?"
: >"${LOG}"
${TMUX} resize-pane -Z -t main >/dev/null 2>&1   # zoom
sleep 0.4
note "zoom:   window-layout-changed +$(logcount window-layout-changed) window-pane-changed +$(logcount window-pane-changed) window-resized +$(logcount window-resized)"
: >"${LOG}"
${TMUX} resize-pane -Z -t main >/dev/null 2>&1   # unzoom
sleep 0.4
note "unzoom: window-layout-changed +$(logcount window-layout-changed) window-pane-changed +$(logcount window-pane-changed) window-resized +$(logcount window-resized)"

########################################################################
say "R4 — ps cost with a large process table"
perl -MTime::HiRes=time -e '
  my $t0=time; system("ps -axo pid=,ppid=,comm= >/dev/null");
  printf "    ps -axo (current table) took %.1f ms\n", (time-$t0)*1000;'
note "spawning 500 background sleeps..."
for i in $(seq 1 500); do sleep 120 & done
SLEEPS=$(jobs -p)
sleep 0.3
perl -MTime::HiRes=time -e '
  my $t0=time; system("ps -axo pid=,ppid=,comm= >/dev/null");
  printf "    ps -axo (+500 procs) took %.1f ms\n", (time-$t0)*1000;'
note "process count now: $(ps -axo pid= | wc -l | tr -d ' ')"
kill ${SLEEPS} 2>/dev/null; wait 2>/dev/null

say "R4 — CPU of a render-tick proxy (ps + tmux list-panes -a @1s)"
note "Phase 9 Loop not built yet; proxy = the per-tick work (ps + list-panes) a"
note "visible sidebar performs each refresh. 15 ticks at 1s, timed with /usr/bin/time:"
${TMUX} new-session -d -s cpu -x 200 -y 50 2>/dev/null
TIMING="${WORK}/cputime.txt"
/usr/bin/time -p bash -c '
  for i in $(seq 1 15); do
    ps -axo pid=,ppid=,comm= >/dev/null 2>&1
    tmux -L '"${SOCK}"' -f /dev/null list-panes -a -F "#{pane_id}" >/dev/null 2>&1
    sleep 1
  done' 2>"${TIMING}"
U=$(awk '/^user/{print $2}' "${TIMING}"); S=$(awk '/^sys/{print $2}' "${TIMING}"); R=$(awk '/^real/{print $2}' "${TIMING}")
note "15 ticks over ${R}s real: user=${U}s sys=${S}s => $(awk "BEGIN{printf \"%.1f\", (${U}+${S})/${R}*100}")% of one core (one visible sidebar)"

say "SPIKE COMPLETE"
note "hook log: ${LOG} (removed on exit)"
