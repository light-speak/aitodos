//go:build !darwin && !linux

package gitworkflow

import (
	"context"
	"errors"
)

type repositoryLock struct{}

func acquireRepositoryLock(context.Context, string) (*repositoryLock, error) {
	return nil, errors.New("当前平台不支持 Git 仓库锁")
}

func (lock *repositoryLock) Close() error { return nil }
