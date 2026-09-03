// Package processidentity 为 Crash Recovery 提供可重复校验的本地进程身份。
package processidentity

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"time"
)

// Matches 判断 PID 当前身份是否仍与 Runner 启动时一致。
func Matches(ctx context.Context, pid int, expected string) bool {
	if len(expected) != 64 {
		return false
	}
	actual, err := Read(ctx, pid)
	return err == nil && actual == expected
}

// TerminateGroup 仅在进程身份仍匹配时终止该进程组。
func TerminateGroup(ctx context.Context, pid int, expected string) error {
	if !Matches(ctx, pid, expected) {
		return errors.New("agent process identity mismatch")
	}
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("terminate agent process group: %w", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(-pid, 0); errors.Is(err, syscall.ESRCH) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return fmt.Errorf("kill agent process group: %w", err)
	}
	return nil
}
