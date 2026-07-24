//go:build darwin || linux

package attachlink

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestValidateReadyURL(t *testing.T) {
	valid := "http://127.0.0.1:49152/attach/Abc_123-opaque-token-xx"
	if got, err := validateReadyURL(valid); err != nil || got != valid {
		t.Fatalf("validateReadyURL(valid) = %q, %v", got, err)
	}
	for _, raw := range []string{
		"https://127.0.0.1:49152/attach/Abc_123-opaque-token-xx",
		"http://localhost:49152/attach/Abc_123-opaque-token-xx",
		"http://127.0.0.1/attach/Abc_123-opaque-token-xx",
		"http://127.0.0.1:49152/attach/short",
		"http://127.0.0.1:49152/other/Abc_123-opaque-token-xx",
		"http://127.0.0.1:49152/attach/Abc_123-opaque-token-xx?x=1",
		"http://127.0.0.1:65536/attach/Abc_123-opaque-token-xx",
	} {
		t.Run(raw, func(t *testing.T) {
			if got, err := validateReadyURL(raw); err == nil || got != "" {
				t.Fatalf("validateReadyURL(%q) = %q, %v; want rejection", raw, got, err)
			}
		})
	}
}

func TestReadReady(t *testing.T) {
	for _, tt := range []struct {
		name string
		in   string
		want string
	}{
		{name: "ready", in: "READY http://127.0.0.1:1/attach/token\n", want: "http://127.0.0.1:1/attach/token"},
		{name: "wrong prefix", in: "http://127.0.0.1:1/attach/token\n"},
		{name: "empty", in: ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := make(chan string, 1)
			readReady(strings.NewReader(tt.in), out)
			if got := <-out; got != tt.want {
				t.Fatalf("readReady() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestLaunchSubprocessTransfersTargetAndCleansUp(t *testing.T) {
	oldTimeout := startupTimeout
	startupTimeout = 5 * time.Second
	defer func() { startupTimeout = oldTimeout }()
	script := t.TempDir() + "/helper.sh"
	if err := os.WriteFile(script, []byte("#!/bin/sh\nread target\nprintf '%s' \"$target\" > \"$TWM_ATTACH_MARKER\"\nprintf '%s\\n' 'READY http://127.0.0.1:49152/attach/Abc_123-opaque-token-xx'\nsleep 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	marker := t.TempDir() + "/target"
	t.Setenv("TWM_ATTACH_MARKER", marker)
	h, err := Launch(context.Background(), script, "%20")
	if err != nil {
		t.Fatalf("Launch() error = %v", err)
	}
	if h.URL() != "http://127.0.0.1:49152/attach/Abc_123-opaque-token-xx" {
		t.Fatalf("URL() = %q", h.URL())
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "%20" {
		t.Fatalf("helper target = %q, error = %v", data, err)
	}
	if err := h.Cancel(); err != nil && !errors.Is(err, os.ErrPermission) {
		t.Fatalf("Cancel() error = %v", err)
	}
	if err := h.Cancel(); err != nil {
		t.Fatalf("second Cancel() error = %v", err)
	}
}
