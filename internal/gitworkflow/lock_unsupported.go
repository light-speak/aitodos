//go:build !darwin

package gitworkflow

import (
	"context"
	"errors"
)

type repositoryLock struct{}

func acquireRepositoryLock(context.Context, string) (*repositoryLock, error) {
	return nil, errors.New("当前版本只支持 macOS Git 仓库锁")
}

func (lock *repositoryLock) Close() error { return nil }
