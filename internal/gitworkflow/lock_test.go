package gitworkflow

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestRepositoryLockWaitsForOwnerAndHonorsContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "git.lock")
	first, err := acquireRepositoryLock(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := acquireRepositoryLock(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second acquire error = %v, want %v", err, context.DeadlineExceeded)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}

	second, err := acquireRepositoryLock(context.Background(), path)
	if err != nil {
		t.Fatalf("reacquire: %v", err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
}
