//go:build linux

package processidentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Read 使用 procfs 的进程启动 tick 和命令名生成身份。
func Read(ctx context.Context, pid int) (string, error) {
	if pid < 1 {
		return "", errors.New("process PID must be positive")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	content, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", fmt.Errorf("read process identity: %w", err)
	}
	closing := strings.LastIndexByte(string(content), ')')
	if closing < 0 {
		return "", errors.New("invalid proc stat")
	}
	fields := strings.Fields(string(content[closing+1:]))
	if len(fields) < 20 {
		return "", errors.New("incomplete proc stat")
	}
	hash := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s", pid, string(content[:closing+1]), fields[19])))
	return hex.EncodeToString(hash[:]), nil
}
