//go:build darwin

package processidentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/unix"
)

// Read 返回包含 PID、内核进程启动时间和命令名的哈希，不调用外部命令。
func Read(ctx context.Context, pid int) (string, error) {
	if pid < 1 {
		return "", errors.New("process PID must be positive")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	process, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", fmt.Errorf("read process identity: %w", err)
	}
	if int(process.Proc.P_pid) != pid {
		return "", errors.New("process identity PID mismatch")
	}
	command := make([]byte, 0, len(process.Proc.P_comm))
	for _, value := range process.Proc.P_comm {
		if value == 0 {
			break
		}
		command = append(command, byte(value))
	}
	raw := fmt.Sprintf("%d:%d:%d:%s", pid, process.Proc.P_starttime.Sec,
		process.Proc.P_starttime.Usec, strings.TrimSpace(string(command)))
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:]), nil
}
