package notify

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const configFileName = "twm.toml"

// ErrConfigFile means the optional twm TOML file could not be read or parsed.
// It deliberately omits the path and parser detail so malformed secret values
// cannot reach hook diagnostics.
var ErrConfigFile = errors.New("telegram config file could not be loaded")

type fileConfig struct {
	Telegram struct {
		BotToken string `toml:"bot_token"`
		ChatID   string `toml:"chat_id"`
	} `toml:"telegram"`
}

// LoadConfig loads Telegram credentials from the environment and the twm
// config file. Non-empty environment values override the corresponding TOML
// values. A complete environment configuration is returned without reading the
// file, preserving the original environment-only behavior.
//
// The file is $XDG_CONFIG_HOME/twm.toml when XDG_CONFIG_HOME is set, otherwise
// ~/.config/twm.toml. A missing file is equivalent to an empty file.
func LoadConfig() (cfg Config, enabled bool, err error) {
	if envConfig, envEnabled, envErr := ConfigFromEnv(); envErr == nil && envEnabled {
		return envConfig, true, nil
	}
	path, err := configPath()
	if err != nil {
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
	if envToken != "" && envChat != "" {
		return validateConfig(envToken, envChat)
	}

	var file fileConfig
	data, readErr := os.ReadFile(path)
	switch {
	case readErr == nil:
		if err := toml.Unmarshal(data, &file); err != nil {
			return Config{}, false, ErrConfigFile
		}
	case os.IsNotExist(readErr):
		// Missing optional config is the same as no file configuration.
	default:
		return Config{}, false, ErrConfigFile
	}

	token := strings.TrimSpace(file.Telegram.BotToken)
	chat := strings.TrimSpace(file.Telegram.ChatID)
	if envToken != "" {
		token = envToken
	}
	if envChat != "" {
		chat = envChat
	}
	return validateConfig(token, chat)
}
