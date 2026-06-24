package preview

import "testing"

func TestTrimTrailingBlankLines(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", ""},
		{"a\nb\n", "a\nb\n"},
		{"a\n\n\n", "a\n"},
		{"\n\n", ""},
		{"a\n\nb\n\n\n", "a\n\nb\n"},
	}
	for _, tt := range tests {
		if got := trimTrailingBlankLines(tt.in); got != tt.want {
			t.Errorf("trimTrailingBlankLines(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
