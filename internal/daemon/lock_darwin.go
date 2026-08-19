//go:build darwin

package daemon

import (
	"errors"
	"os"
	"syscall"
)

var errAlreadyRunning = errors.New("项目已经在另一个终端运行")

type fileLock struct {
	file *os.File
}

func acquireFileLock(path string) (*fileLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errAlreadyRunning
		}
		return nil, err
	}
	return &fileLock{file: file}, nil
}

func (lock *fileLock) Close() error {
	unlockErr := syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN)
	closeErr := lock.file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
