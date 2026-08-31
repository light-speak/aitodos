package daemon

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestFileLockPreventsConcurrentDaemonAndCanBeReacquired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.lock")
	first, err := acquireFileLock(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := acquireFileLock(path); !errors.Is(err, errAlreadyRunning) {
		t.Fatalf("second acquire error = %v, want %v", err, errAlreadyRunning)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := acquireFileLock(path)
	if err != nil {
		t.Fatalf("reacquire: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}
