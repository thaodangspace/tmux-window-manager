package notify

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfigFileAndEnvironmentPrecedence(t *testing.T) {
	const (
		fileToken = "123:file-secret"
		fileChat  = "-100-file"
		envToken  = "456:env-secret"
		envChat   = "-100-env"
	)

	tests := []struct {
		name       string
		contents   *string
		env        map[string]string
		want       Config
		enabled    bool
		wantErr    error
		unreadable bool
	}{
		{name: "missing file is disabled"},
		{name: "file config", contents: strPtr("[telegram]\nbot_token = \"  " + fileToken + "  \"\nchat_id = \" " + fileChat + " \"\n"), want: Config{BotToken: fileToken, ChatID: fileChat}, enabled: true},
		{name: "partial file", contents: strPtr("[telegram]\nbot_token = \"" + fileToken + "\"\n"), wantErr: ErrPartialConfig},
		{name: "environment token overrides file", contents: strPtr("[telegram]\nbot_token = \"" + fileToken + "\"\nchat_id = \"" + fileChat + "\"\n"), env: map[string]string{botTokenEnv: envToken}, want: Config{BotToken: envToken, ChatID: fileChat}, enabled: true},
		{name: "environment chat completes file", contents: strPtr("[telegram]\nbot_token = \"" + fileToken + "\"\n"), env: map[string]string{chatIDEnv: envChat}, want: Config{BotToken: fileToken, ChatID: envChat}, enabled: true},
		{name: "complete environment skips malformed file", contents: strPtr("not valid = [toml"), env: map[string]string{botTokenEnv: envToken, chatIDEnv: envChat}, want: Config{BotToken: envToken, ChatID: envChat}, enabled: true},
		{name: "malformed file", contents: strPtr("[telegram\nbot_token = \"never-leak-secret\""), wantErr: ErrConfigFile},
		{name: "unreadable path", unreadable: true, wantErr: ErrConfigFile},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, configFileName)
			if tt.contents != nil {
				if err := os.WriteFile(path, []byte(*tt.contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tt.unreadable {
				path = dir // reading a directory fails on every supported platform
			}
			lookup := func(key string) string { return tt.env[key] }

			got, enabled, err := loadConfig(path, lookup)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			if enabled != tt.enabled || got != tt.want {
				t.Fatalf("loadConfig() = %+v, %v; want %+v, %v", got, enabled, tt.want, tt.enabled)
			}
			if err != nil {
				for _, secret := range []string{fileToken, envToken, "never-leak-secret"} {
					if strings.Contains(err.Error(), secret) {
						t.Fatalf("error leaked credential %q: %v", secret, err)
					}
				}
			}
		})
	}
}

func TestConfigPath(t *testing.T) {
	t.Run("XDG config home", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", dir)
		got, err := configPath()
		if err != nil || got != filepath.Join(dir, configFileName) {
			t.Fatalf("configPath() = %q, %v", got, err)
		}
	})

	t.Run("home fallback", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", home)
		got, err := configPath()
		if err != nil || got != filepath.Join(home, ".config", configFileName) {
			t.Fatalf("configPath() = %q, %v", got, err)
		}
	})
}

func TestLoadConfigUsesConfiguredPath(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv(botTokenEnv, "")
	t.Setenv(chatIDEnv, "")
	contents := "[telegram]\nbot_token = \"token\"\nchat_id = \"chat\"\n"
	if err := os.WriteFile(filepath.Join(dir, configFileName), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, enabled, err := LoadConfig()
	if err != nil || !enabled || cfg != (Config{BotToken: "token", ChatID: "chat"}) {
		t.Fatalf("LoadConfig() = %+v, %v, %v", cfg, enabled, err)
	}
}

func strPtr(s string) *string { return &s }
