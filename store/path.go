// Package store is the SQLite persistence layer for event-driven agent status.
// Agents push lifecycle status (idle/running/waiting/ended) into this DB via the
// `twm hook` subcommand; the picker reads it back. It replaces the old polling
// path (capture-pane busy regex + the transcript JSON cache).
package store

import (
	"os"
	"path/filepath"
)

// Path returns the canonical DB path that both the hook writers and the picker
// reader resolve through, so they always agree on one file:
//
//	$TWM_DB_PATH                                  (explicit override; tests/debug)
//	$XDG_STATE_HOME/tmux-window-manager/agents.db
//	~/.local/state/tmux-window-manager/agents.db  (fallback)
//
// The parent directory is created with 0700 perms.
func Path() (string, error) {
	if p := os.Getenv("TWM_DB_PATH"); p != "" {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return "", err
		}
		return p, nil
	}
	dir := stateDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "agents.db"), nil
}

func stateDir() string {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, "tmux-window-manager")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.TempDir()
	}
	return filepath.Join(home, ".local", "state", "tmux-window-manager")
}
