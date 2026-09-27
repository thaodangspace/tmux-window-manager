#!/usr/bin/env bash
# TPM entry point for tmux-window-manager.
#
# On every tmux start TPM runs this script. It builds the Go binary on first
# install (or after a `git pull` changes the source), exposes the binary path as
# the @twm_bin tmux option (so status-bar formats can call it), and binds the
# picker key (default: prefix + w, override with `set -g @twm_key '<key>'`).
set -euo pipefail

CURRENT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]:-$0}")" && pwd)"
BIN="$CURRENT_DIR/bin/tmux-window-manager"

# TPM runs hooks with a minimal environment; make sure the Go toolchain and
# common binary dirs (fzf lives here too) are discoverable.
export PATH="/opt/homebrew/bin:/usr/local/bin:/usr/local/go/bin:$HOME/go/bin:$PATH"

msg() { tmux display-message "tmux-window-manager: $1"; }

# Rebuild when the binary is missing or any source file is newer than it.
needs_build() {
  [ ! -x "$BIN" ] && return 0
  [ -n "$(find "$CURRENT_DIR" -name '*.go' -newer "$BIN" -print -quit 2>/dev/null)" ]
}

if needs_build; then
  if command -v go >/dev/null 2>&1; then
    if ! (cd "$CURRENT_DIR" && go build -o "$BIN" ./cmd/tmux-window-manager) >/tmp/twm_build.log 2>&1; then
      msg "build failed — see /tmp/twm_build.log"
      exit 0
    fi
  elif [ ! -x "$BIN" ]; then
    msg "Go toolchain not found and no prebuilt binary — install Go to use this plugin"
    exit 0
  fi
fi

# Publish the binary path so other config (e.g. window-status-format) can invoke
# it without hardcoding the plugin location: #(#{@twm_bin} label ...).
tmux set-option -g @twm_bin "$BIN"

# Clean up panes, hooks, and options left by versions that shipped the removed
# persistent sidebar feature. Only the plugin-owned hook slot (90) is touched.
for hook in after-new-window session-created after-split-window window-linked \
  window-unlinked pane-exited after-kill-pane window-layout-changed \
  window-resized client-resized session-window-changed client-session-changed \
  after-select-window window-pane-changed; do
  tmux set-hook -gu "${hook}[90]" 2>/dev/null || true
done
while IFS= read -r pane; do
  [ -n "$pane" ] && tmux kill-pane -t "$pane" 2>/dev/null || true
done < <(tmux list-panes -a -f '#{m:* sidebar render*,#{pane_start_command}}' -F '#{pane_id}' 2>/dev/null || true)
tmux set-option -gu @twm_sidebar_enabled 2>/dev/null || true
while IFS= read -r window; do
  [ -n "$window" ] && tmux set-option -wu -t "$window" @twm_sidebar_off 2>/dev/null || true
done < <(tmux list-windows -a -F '#{window_id}' 2>/dev/null || true)

KEY="$(tmux show-option -gqv @twm_key)"
[ -n "$KEY" ] || KEY="w"
tmux bind-key "$KEY" run-shell -b "$BIN run '#{client_name}'"
