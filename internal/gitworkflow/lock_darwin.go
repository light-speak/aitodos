//go:build darwin

package gitworkflow

import (
	"context"
	"fmt"
	"os"
	"syscall"
	"time"
)

type repositoryLock struct {
	file *os.File
}

func acquireRepositoryLock(ctx context.Context, path string) (*repositoryLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open repository lock: %w", err)
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			return &repositoryLock{file: file}, nil
		} else if err != syscall.EWOULDBLOCK {
			file.Close()
			return nil, fmt.Errorf("lock repository: %w", err)
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (lock *repositoryLock) Close() error {
	unlockErr := syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN)
	closeErr := lock.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
