package agents

import "testing"

func TestIsAgent(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"claude", true},
		{"codex", true},
		{"pi", true},
		{"claude-code", false},
		{"Claude", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsAgent(tt.name); got != tt.want {
			t.Errorf("IsAgent(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestDisplayName(t *testing.T) {
	tests := []struct {
		id   string
		want string
	}{
		{"claude", "Claude Code"},
		{"codex", "Codex"},
		{"pi", "pi"},
		{"mystery", "mystery"}, // unknown ids are returned unchanged
	}
	for _, tt := range tests {
		if got := DisplayName(tt.id); got != tt.want {
			t.Errorf("DisplayName(%q) = %q, want %q", tt.id, got, tt.want)
		}
	}
}
