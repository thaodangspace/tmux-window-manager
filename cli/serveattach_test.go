package cli

import (
	"strings"
	"testing"
)

func TestReadAttachTarget(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{name: "valid", in: "%20\n", want: "%20", ok: true},
		{name: "valid without newline", in: "%0", want: "%0", ok: true},
		{name: "invalid target", in: "session:window\n"},
		{name: "empty", in: "\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readAttachTarget(strings.NewReader(tt.in))
			if tt.ok {
				if err != nil || got != tt.want {
					t.Fatalf("readAttachTarget() = %q, %v; want %q", got, err, tt.want)
				}
				return
			}
			if err == nil {
				t.Fatalf("readAttachTarget(%q) unexpectedly succeeded", tt.in)
			}
		})
	}
}

func TestServeAttachCommandIsHiddenAndArgumentless(t *testing.T) {
	cmd := newServeAttachCommand()
	if !cmd.Hidden {
		t.Fatal("serve-attach is not hidden")
	}
	if err := cmd.Args(cmd, []string{"%20"}); err == nil {
		t.Fatal("serve-attach accepted a target argument")
	}
}
