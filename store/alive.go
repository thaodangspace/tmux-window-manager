//go:build unix

package store

import (
	"errors"
	"syscall"
)

// alive reports whether a process id is still running. A pid of 0 (or negative)
// means "unknown" and is treated as live so we never hide an agent just because
// its hook couldn't resolve a pid. signal 0 performs existence/permission
// checks without delivering a signal: nil or EPERM => the process exists; ESRCH
// => it is gone.
func alive(pid int) bool {
	if pid <= 0 {
		return true
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}
