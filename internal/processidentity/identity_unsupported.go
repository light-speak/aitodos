//go:build !darwin && !linux

package processidentity

import (
	"context"
	"errors"
)

// Read 在未实现内核进程身份的平台上明确失败，禁止降级为仅 PID。
func Read(context.Context, int) (string, error) {
	return "", errors.New("process identity is not supported on this platform")
}
