//go:build !darwin

package daemon

import "errors"

var errAlreadyRunning = errors.New("项目已经在另一个终端运行")

type fileLock struct{}

func acquireFileLock(string) (*fileLock, error) {
	return nil, errors.New("当前版本只支持 macOS")
}

func (*fileLock) Close() error {
	return nil
}
