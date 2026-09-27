//go:build unix

package sidebar

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLockAtCreatesOwnerOnlyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twm-sidebar-test.lock")
	l, err := lockAt(path, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("lockAt: %v", err)
	}
	defer l.Release()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat lock file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("lock file mode = %o, want 600", perm)
	}
}

func TestLockAtRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	realFile := filepath.Join(dir, "real")
	if err := os.WriteFile(realFile, nil, 0o600); err != nil {
		t.Fatalf("write real file: %v", err)
	}
	link := filepath.Join(dir, "link.lock")
	if err := os.Symlink(realFile, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	l, err := lockAt(link, 50*time.Millisecond)
	if err == nil {
		l.Release()
		t.Fatal("lockAt followed a symlink; want refusal")
	}
}

func TestLockAtSecondLockerTimesOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "twm-sidebar-test.lock")
	first, err := lockAt(path, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("first lockAt: %v", err)
	}
	defer first.Release()

	start := time.Now()
	second, err := lockAt(path, 50*time.Millisecond)
	if err == nil {
		second.Release()
		t.Fatal("second lockAt succeeded while first held the lock")
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond {
		t.Fatalf("second lockAt returned after %s, want at least the retry window", elapsed)
	}

	// After the first releases, a new locker should succeed promptly.
	if err := first.Release(); err != nil {
		t.Fatalf("release first: %v", err)
	}
	third, err := lockAt(path, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("lockAt after release: %v", err)
	}
	third.Release()
}
