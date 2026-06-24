package agents

import (
	"encoding/json"
	"testing"
	"time"
)

func TestExtractText(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"plain string", `"  hi   there  "`, "hi there"},
		{"block array first non-empty", `[{"text":""},{"text":"  the   answer "}]`, "the answer"},
		{"empty array", `[]`, ""},
		{"null", `null`, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractText(json.RawMessage(tc.in)); got != tc.want {
				t.Errorf("extractText(%s) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCleanCollapsesWhitespace(t *testing.T) {
	if got := clean("  a\n\t b   c "); got != "a b c" {
		t.Errorf("clean = %q, want %q", got, "a b c")
	}
}

func TestParseTimestamp(t *testing.T) {
	want, _ := time.Parse(time.RFC3339Nano, "2026-06-22T10:00:02.000Z")
	if got := parseTimestamp("2026-06-22T10:00:02.000Z"); !got.Equal(want) {
		t.Errorf("parseTimestamp = %v, want %v", got, want)
	}
	if got := parseTimestamp("nonsense"); !got.IsZero() {
		t.Errorf("bad timestamp should be zero, got %v", got)
	}
	if got := parseTimestamp(""); !got.IsZero() {
		t.Errorf("empty timestamp should be zero, got %v", got)
	}
}
