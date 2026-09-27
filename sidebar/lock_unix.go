//go:build unix

package sidebar

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// lockRetry bounds how long Lock waits for a competing ensure to release the
// lock before giving up. Many tmux hooks can fire at once (e.g. tmux-resurrect
// restoring dozens of windows), so ensure runs are serialized.
const lockRetry = 2 * time.Second

// FileLock is an advisory file lock that serializes concurrent `ensure` runs.
// Release drops the lock; the zero value is a no-op.
type FileLock struct {
	f *os.File
}

// Release unlocks and closes the lock file. Closing the fd drops the flock.
func (l *FileLock) Release() error {
	if l == nil || l.f == nil {
		return nil
	}
	err := l.f.Close()
	l.f = nil
	return err
}

// LockPath returns the ensure lock path: a per-uid file in $TMPDIR. The uid in
// the name keeps two users' locks separate and lets O_NOFOLLOW refuse a symlink
// another user planted at the path.
func LockPath() string {
	name := "twm-sidebar-" + strconv.Itoa(os.Getuid()) + ".lock"
	return filepath.Join(os.TempDir(), name)
}

// Lock acquires the ensure lock, retrying non-blocking flock for up to
// lockRetry.
func Lock() (*FileLock, error) {
	return lockAt(LockPath(), lockRetry)
}

// lockAt opens path O_NOFOLLOW (a symlink at the path is refused) with mode
// 0600 (owner-only), then takes a non-blocking exclusive flock, retrying until
// retry elapses. It is the testable core of Lock.
func lockAt(path string, retry time.Duration) (*FileLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open sidebar lock: %w", err)
	}
	deadline := time.Now().Add(retry)
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &FileLock{f: f}, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) {
			f.Close()
			return nil, fmt.Errorf("lock sidebar lock: %w", err)
		}
		if time.Now().After(deadline) {
			f.Close()
			return nil, fmt.Errorf("sidebar lock busy after %s", retry)
		}
		time.Sleep(20 * time.Millisecond)
	}
}
