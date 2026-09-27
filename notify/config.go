package notify

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

const configFileName = "twm.toml"

// ErrConfigFile means the optional twm TOML file could not be read or parsed.
// It deliberately omits the path and parser detail so malformed secret values
// cannot reach hook diagnostics.
var ErrConfigFile = errors.New("telegram config file could not be loaded")

type fileConfig struct {
	Telegram struct {
		BotToken        string `toml:"bot_token"`
		ChatID          string `toml:"chat_id"`
		IncludeResponse *bool  `toml:"include_response"`
		MinTurnSeconds  *int   `toml:"min_turn_seconds"`
		SkipWhenFocused *bool  `toml:"skip_when_focused"`
	} `toml:"telegram"`
}

// Options controls which events are delivered and what they contain. They are
// preferences, not credentials, and are read only from the TOML file.
type Options struct {
	// IncludeResponse adds an excerpt of the agent's last reply to completed
	// turns. Off by default: the text leaves the machine via Telegram.
	IncludeResponse bool
	// MinTurn suppresses completed-turn messages for turns shorter than this.
	// Zero sends every completed turn.
	MinTurn time.Duration
	// SkipWhenFocused suppresses messages while the agent's pane is on screen
	// in a recently active tmux client.
	SkipWhenFocused bool
}

// DefaultOptions are used for any preference the file leaves unset.
func DefaultOptions() Options {
	return Options{MinTurn: 30 * time.Second, SkipWhenFocused: true}
}

func (f fileConfig) options() Options {
	opts := DefaultOptions()
	t := f.Telegram
	if t.IncludeResponse != nil {
		opts.IncludeResponse = *t.IncludeResponse
	}
	if t.MinTurnSeconds != nil && *t.MinTurnSeconds >= 0 {
		opts.MinTurn = time.Duration(*t.MinTurnSeconds) * time.Second
	}
	if t.SkipWhenFocused != nil {
		opts.SkipWhenFocused = *t.SkipWhenFocused
	}
	return opts
}

// LoadConfig loads Telegram credentials from the environment and the twm
// config file. Non-empty environment values override the corresponding TOML
// values. With a complete environment configuration the file only supplies
// Options, and an unreadable or malformed file falls back to DefaultOptions.
//
// The file is $XDG_CONFIG_HOME/twm.toml when XDG_CONFIG_HOME is set, otherwise
// ~/.config/twm.toml. A missing file is equivalent to an empty file.
func LoadConfig() (cfg Config, enabled bool, err error) {
	path, pathErr := configPath()
	if pathErr != nil {
		if envConfig, envEnabled, envErr := ConfigFromEnv(); envErr == nil && envEnabled {
			envConfig.Options = DefaultOptions()
			return envConfig, true, nil
		}
		return Config{}, false, ErrConfigFile
	}
	return loadConfig(path, os.Getenv)
}

func configPath() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); dir != "" {
		return filepath.Join(dir, configFileName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", ErrConfigFile
	}
	return filepath.Join(home, ".config", configFileName), nil
}

func loadConfig(path string, lookup func(string) string) (cfg Config, enabled bool, err error) {
	envToken := strings.TrimSpace(lookup(botTokenEnv))
	envChat := strings.TrimSpace(lookup(chatIDEnv))

	file, fileErr := readFileConfig(path)
	if envToken != "" && envChat != "" {
		// Credentials are complete without the file; it only supplies Options.
		opts := DefaultOptions()
		if fileErr == nil {
			opts = file.options()
		}
		return enable(envToken, envChat, opts)
	}
	if fileErr != nil {
		return Config{}, false, fileErr
	}

	token := strings.TrimSpace(file.Telegram.BotToken)
	chat := strings.TrimSpace(file.Telegram.ChatID)
	if envToken != "" {
		token = envToken
	}
	if envChat != "" {
		chat = envChat
	}
	return enable(token, chat, file.options())
}

// readFileConfig parses the optional TOML file. A missing file is an empty
// configuration; any other failure is ErrConfigFile.
func readFileConfig(path string) (fileConfig, error) {
	var file fileConfig
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if toml.Unmarshal(data, &file) != nil {
			return fileConfig{}, ErrConfigFile
		}
	case os.IsNotExist(err):
	default:
		return fileConfig{}, ErrConfigFile
	}
	return file, nil
}

func enable(token, chat string, opts Options) (Config, bool, error) {
	cfg, enabled, err := validateConfig(token, chat)
	if enabled {
		cfg.Options = opts
	}
	return cfg, enabled, err
}
