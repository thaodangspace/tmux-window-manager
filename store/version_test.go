package store

import (
	"path/filepath"
	"testing"
)

// TestDataVersionChangesAfterExternalWrite verifies that a write committed by a
// second connection bumps the value read from the first, while a quiet read is
// stable.
func TestDataVersionChangesAfterExternalWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.db")

	reader, err := OpenAt(path)
	if err != nil {
		t.Fatalf("OpenAt reader: %v", err)
	}
	defer reader.Close()

	before, err := reader.DataVersion()
	if err != nil {
		t.Fatalf("DataVersion before: %v", err)
	}

	// A second read with no intervening write must be stable.
	stable, err := reader.DataVersion()
	if err != nil {
		t.Fatalf("DataVersion stable: %v", err)
	}
	if stable != before {
		t.Fatalf("DataVersion changed without a write: %d -> %d", before, stable)
	}

	writer, err := OpenAt(path)
	if err != nil {
		t.Fatalf("OpenAt writer: %v", err)
	}
	defer writer.Close()
	if err := writer.Upsert(mk("claude", "s1", "/tmp", 1, Running, 1)); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	after, err := reader.DataVersion()
	if err != nil {
		t.Fatalf("DataVersion after: %v", err)
	}
	if after == before {
		t.Fatalf("DataVersion did not change after external write: still %d", after)
	}
}
