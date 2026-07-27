---
title: Getting started
description: Install the TPM plugin, build it locally, and open the picker.
---

## Requirements

- macOS or Linux
- tmux 3.2 or newer (`display-popup` is required)
- `fzf`
- Go 1.25 or newer for the plugin's build-on-install step

The Go binary is cgo-free. Its SQLite driver does not require a C compiler.

## Install with TPM

Add the plugin to `~/.tmux.conf`:

```text
set -g @plugin 'thaodangspace/tmux-window-manager'
```

Reload tmux if needed, then press `prefix + I`. TPM clones the repository and
runs `tmux-window-manager.tmux`, which builds the binary when it is missing or
older than a Go source file. The plugin also binds `prefix + w` and publishes the
binary path through the `@twm_bin` tmux option.

Press `prefix + w` to open the picker.

## Install from a local checkout

Point tmux at the plugin entrypoint:

```text
run-shell ~/code/tmux-window-manager/tmux-window-manager.tmux
```

This uses the same build-on-install behavior and key binding as TPM.

## Build the binary directly

```sh
go build -o bin/tmux-window-manager ./cmd/tmux-window-manager
```

Run the picker from inside tmux:

```sh
./bin/tmux-window-manager run
```

The plugin primes common Homebrew paths because tmux `run-shell` often has a
minimal `PATH`. If `fzf` still cannot be found, ensure it is installed in a
standard path or make it visible to the tmux server environment.

## Next steps

- Learn the [window picker controls](/window-picker/).
- Install [agent status hooks](/agent-status/).
- Add [Telegram notifications](/telegram/) only if you want them.
