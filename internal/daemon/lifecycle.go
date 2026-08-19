package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"syscall"
	"time"

	"github.com/light-speak/aitodos/internal/project"
)

// Status 验证 metadata 对应的项目服务是否仍然健康。
func Status(ctx context.Context, currentProject *project.Project) (Metadata, bool) {
	metadata, err := readMetadata(currentProject.Paths.DaemonState)
	if err != nil || metadata.ProjectInstanceID != currentProject.InstanceID {
		return Metadata{}, false
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, metadata.URL+"/api/health", nil)
	if err != nil {
		return Metadata{}, false
	}
	client := &http.Client{Timeout: 500 * time.Millisecond}
	response, err := client.Do(request)
	if err != nil {
		return Metadata{}, false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return Metadata{}, false
	}
	var health Health
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		return Metadata{}, false
	}
	if health.Status != "ok" ||
		health.ProjectInstanceID != metadata.ProjectInstanceID ||
		health.Nonce != metadata.Nonce ||
		health.PID != metadata.PID {
		return Metadata{}, false
	}
	return metadata, true
}

// Stop 优雅停止当前项目前台服务。
func Stop(ctx context.Context, currentProject *project.Project) (bool, error) {
	metadata, running := Status(ctx, currentProject)
	if !running {
		return false, nil
	}
	process, err := os.FindProcess(metadata.PID)
	if err != nil {
		return false, fmt.Errorf("find daemon process: %w", err)
	}
	if err := process.Signal(syscall.SIGTERM); err != nil {
		return false, fmt.Errorf("stop daemon process: %w", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, running := Status(ctx, currentProject); !running {
			return true, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	return false, errors.New("项目服务未在 5 秒内停止")
}
