// Package processidentity 为 Crash Recovery 提供可重复校验的本地进程身份。
package processidentity

import "context"

// Matches 判断 PID 当前身份是否仍与 Runner 启动时一致。
func Matches(ctx context.Context, pid int, expected string) bool {
	if len(expected) != 64 {
		return false
	}
	actual, err := Read(ctx, pid)
	return err == nil && actual == expected
}
