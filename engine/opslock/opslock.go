// Package opslock provides per-app operation locks to prevent concurrent
// boxx operations (manual deploy, auto-update, env change) from racing.
//
// Locks are backed by flock(2) on per-app files under $BOXX_HOME/locks/.
// They are automatically released when the process exits, so a crashed boxx
// invocation never permanently wedges an app.
package opslock

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/plainwork/boxx/engine/state"
)

// Acquire obtains an exclusive, non-blocking lock for the given slug.
// Returns an unlock func (call defer unlock()) and nil on success.
// Returns an error immediately if another operation already holds the lock.
func Acquire(slug string) (unlock func(), err error) {
	dir := filepath.Join(state.Root(), "locks")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("opslock: mkdir %s: %w", dir, err)
	}
	path := filepath.Join(dir, sanitize(slug)+".lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opslock: open %s: %w", path, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("app %q is busy — another operation is already in progress", slug)
	}
	return func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck
		f.Close()
	}, nil
}

// sanitize replaces any character that isn't safe in a filename with _.
func sanitize(s string) string {
	b := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			b[i] = c
		} else {
			b[i] = '_'
		}
	}
	return string(b)
}
