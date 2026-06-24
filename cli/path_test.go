package cli

import (
	"os"
	"strings"
	"testing"
)

func TestEnsurePath(t *testing.T) {
	t.Run("prepends missing dirs", func(t *testing.T) {
		t.Setenv("PATH", "/usr/bin")
		ensurePath()
		got := os.Getenv("PATH")
		if !strings.HasPrefix(got, "/opt/homebrew/bin"+string(os.PathListSeparator)) {
			t.Errorf("PATH should start with homebrew bin, got %q", got)
		}
		if !strings.HasSuffix(got, "/usr/bin") {
			t.Errorf("PATH should preserve original tail, got %q", got)
		}
	})

	t.Run("does not duplicate existing dirs", func(t *testing.T) {
		t.Setenv("PATH", "/opt/homebrew/bin"+string(os.PathListSeparator)+"/usr/bin")
		ensurePath()
		got := os.Getenv("PATH")
		if strings.Count(got, "/opt/homebrew/bin") != 1 {
			t.Errorf("homebrew bin should appear once, got %q", got)
		}
	})
}
