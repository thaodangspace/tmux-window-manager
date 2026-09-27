// Package config loads the optional twm.toml configuration for the sidebar.
package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const configFileName = "twm.toml"

// ErrConfig means the optional twm TOML file could not be read or parsed. It
// deliberately omits the path and parser detail so malformed secret values
// (the same file also holds the Telegram token) cannot reach diagnostics.
var ErrConfig = errors.New("sidebar config file could not be loaded")

// Clamp bounds for the sidebar geometry and refresh interval.
const (
	minWidth     = 20
	maxWidth     = 80
	minRefreshMS = 500
	maxRefreshMS = 10000
)

// Sidebar holds the resolved, clamped sidebar preferences.
type Sidebar struct {
	Enabled   bool
	Width     int
	RefreshMS int
}

// DefaultSidebar returns the defaults used when the file is missing, malformed,
// or leaves a field unset.
func DefaultSidebar() Sidebar {
	return Sidebar{Enabled: true, Width: 32, RefreshMS: 1000}
}

// fileConfig decodes only the [sidebar] table, so [telegram] (which holds the
// bot token) is never retained by this package. Pointers distinguish an absent
// key from a zero value, preserving the default.
type fileConfig struct {
	Sidebar struct {
		Enabled   *bool `toml:"enabled"`
		Width     *int  `toml:"width"`
		RefreshMS *int  `toml:"refresh_ms"`
	} `toml:"sidebar"`
}

func (f fileConfig) sidebar() Sidebar {
	s := DefaultSidebar()
	sb := f.Sidebar
	if sb.Enabled != nil {
		s.Enabled = *sb.Enabled
	}
	if sb.Width != nil {
		s.Width = clamp(*sb.Width, minWidth, maxWidth)
	}
	if sb.RefreshMS != nil {
		s.RefreshMS = clamp(*sb.RefreshMS, minRefreshMS, maxRefreshMS)
	}
	return s
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Path returns the twm.toml location: $XDG_CONFIG_HOME/twm.toml when
// XDG_CONFIG_HOME is set, otherwise ~/.config/twm.toml. This mirrors
// notify.configPath (duplicated to keep notify/ untouched).
func Path() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); dir != "" {
		return filepath.Join(dir, configFileName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", ErrConfig
	}
	return filepath.Join(home, ".config", configFileName), nil
}

// Load reads the sidebar config from the canonical path, returning the resolved
// settings and the path consulted. A missing file yields defaults and a nil
// error; a malformed or unreadable file yields defaults and ErrConfig.
func Load() (Sidebar, string, error) {
	path, err := Path()
	if err != nil {
		return DefaultSidebar(), "", ErrConfig
	}
	cfg, err := LoadFrom(path)
	return cfg, path, err
}

// LoadFrom reads the sidebar config from an explicit path. A missing file
// yields defaults and a nil error; any other read or parse failure yields
// defaults and ErrConfig.
func LoadFrom(path string) (Sidebar, error) {
	var file fileConfig
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if toml.Unmarshal(data, &file) != nil {
			return DefaultSidebar(), ErrConfig
		}
	case os.IsNotExist(err):
		return DefaultSidebar(), nil
	default:
		return DefaultSidebar(), ErrConfig
	}
	return file.sidebar(), nil
}

// ResolveEnabled applies the precedence runtime > file: the tmux option
// @twm_sidebar_enabled ("1" on, "0" off) wins, and anything else falls back to
// the file value.
func ResolveEnabled(runtime string, file Sidebar) bool {
	switch strings.TrimSpace(runtime) {
	case "1":
		return true
	case "0":
		return false
	default:
		return file.Enabled
	}
}
