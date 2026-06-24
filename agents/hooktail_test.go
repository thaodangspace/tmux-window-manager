package agents

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranscriptTailModelAndLatest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.jsonl")
	lines := []string{
		`{"type":"user","timestamp":"2026-06-23T10:00:00.000Z","message":{"role":"user","content":"hello"}}`,
		`{"type":"assistant","timestamp":"2026-06-23T10:00:01.000Z","message":{"role":"assistant","model":"opus-4","content":[{"text":"first reply"}]}}`,
		`{"type":"assistant","timestamp":"2026-06-23T10:00:02.000Z","message":{"role":"assistant","model":"opus-4","content":[{"text":"latest reply"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	model, latest := TranscriptTail(path)
	if model != "opus-4" {
		t.Fatalf("model = %q", model)
	}
	if latest != "latest reply" {
		t.Fatalf("latest = %q", latest)
	}
}

func TestTranscriptTailModelPreference(t *testing.T) {
	// When both top-level "model" and message.model are present, the top-level
	// (friendly display name) wins over the full API identifier.
	dir := t.TempDir()
	path := filepath.Join(dir, "x.jsonl")
	lines := []string{
		`{"type":"assistant","model":"sonnet","timestamp":"2026-06-23T10:00:01.000Z","message":{"role":"assistant","model":"claude-sonnet-4-20250514","content":[{"text":"first reply"}]}}`,
		`{"type":"assistant","model":"sonnet","timestamp":"2026-06-23T10:00:02.000Z","message":{"role":"assistant","model":"claude-sonnet-4-20250514","content":[{"text":"latest reply"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	model, latest := TranscriptTail(path)
	if model != "sonnet" {
		t.Fatalf("model should prefer top-level field, got %q", model)
	}
	if latest != "latest reply" {
		t.Fatalf("latest = %q", latest)
	}
}

func TestTranscriptTailModelFallback(t *testing.T) {
	// When only message.model is present (no top-level model), fall back to it.
	dir := t.TempDir()
	path := filepath.Join(dir, "x.jsonl")
	lines := []string{
		`{"type":"assistant","timestamp":"2026-06-23T10:00:01.000Z","message":{"role":"assistant","model":"opus-4","content":[{"text":"reply"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	model, _ := TranscriptTail(path)
	if model != "opus-4" {
		t.Fatalf("model should fall back to message.model, got %q", model)
	}
}

func TestTranscriptTailMissingFile(t *testing.T) {
	m, l := TranscriptTail(filepath.Join(t.TempDir(), "nope.jsonl"))
	if m != "" || l != "" {
		t.Fatalf("missing file should yield empty, got %q/%q", m, l)
	}
}

func TestNearestAgent(t *testing.T) {
	// 100(claude) -> 200(node) -> 300(hook). Walking up from 300 finds claude.
	snap := "100 1 /usr/bin/claude\n200 100 node\n300 200 twm\n"
	d := newDetectorFromSnapshot(snap)
	pid, name := d.NearestAgent("300")
	if pid != 100 || name != "claude" {
		t.Fatalf("NearestAgent = %d/%q, want 100/claude", pid, name)
	}
	// No agent in the chain.
	d2 := newDetectorFromSnapshot("10 1 bash\n20 10 node\n")
	if pid, name := d2.NearestAgent("20"); pid != 0 || name != "" {
		t.Fatalf("expected no agent, got %d/%q", pid, name)
	}
}
