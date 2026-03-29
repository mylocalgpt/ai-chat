//go:build unix

package lockfile

import (
	"fmt"
	"io"
	"os"
	"syscall"
)

// Lock acquires a non-blocking exclusive file lock on path.
// On success it returns an io.Closer that releases the lock and closes the file.
// If another process already holds the lock, it returns an error immediately.
func Lock(path string) (io.Closer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("opening lock file: %w", err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("another ai-chat instance is already polling Telegram (lock held on %s)", path)
	}

	return &lockCloser{f: f}, nil
}

type lockCloser struct {
	f *os.File
}

func (l *lockCloser) Close() error {
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	return l.f.Close()
}
