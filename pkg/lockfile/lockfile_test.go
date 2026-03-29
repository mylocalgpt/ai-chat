//go:build unix

package lockfile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLock_succeeds_on_fresh_file(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")

	closer, err := Lock(path)
	if err != nil {
		t.Fatalf("Lock() returned unexpected error: %v", err)
	}
	defer func() { _ = closer.Close() }()

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("lock file was not created: %v", err)
	}
}

func TestLock_second_lock_fails_while_first_held(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")

	first, err := Lock(path)
	if err != nil {
		t.Fatalf("first Lock() failed: %v", err)
	}
	defer func() { _ = first.Close() }()

	_, err = Lock(path)
	if err == nil {
		t.Fatal("second Lock() should have returned an error while first is held")
	}
}

func TestLock_closing_first_allows_second(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.lock")

	first, err := Lock(path)
	if err != nil {
		t.Fatalf("first Lock() failed: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("first Close() failed: %v", err)
	}

	second, err := Lock(path)
	if err != nil {
		t.Fatalf("second Lock() after close should succeed, got: %v", err)
	}
	defer func() { _ = second.Close() }()
}
