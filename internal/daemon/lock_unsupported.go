//go:build !darwin && !linux

package daemon

import "errors"

var errAlreadyRunning = errors.New("项目已经在另一个终端运行")

type fileLock struct{}

func acquireFileLock(string) (*fileLock, error) {
	return nil, errors.New("当前平台不支持 Daemon 文件锁")
}

func (*fileLock) Close() error {
	return nil
}
