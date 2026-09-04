// Package recovery 对账 Daemon 启动前遗留的 Runner、Run 和 Workspace。
package recovery

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"time"

	"github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/gitworkflow"
	"github.com/light-speak/aitodos/internal/processidentity"
	"github.com/light-speak/aitodos/internal/project"
	"github.com/light-speak/aitodos/internal/runner"
	"github.com/light-speak/aitodos/internal/storage"
)

// Manager 只跟踪 Daemon 启动时已经存在的非终态 Run，避免与当前 Scheduler 竞争。
type Manager struct {
	project             *project.Project
	database            *sql.DB
	store               *storage.RunStore
	matchesProcess      func(context.Context, int, string) bool
	terminateAgent      func(context.Context, int, string) error
	recoverFinalization func(context.Context, *project.Project, *sql.DB, storage.RecoveryRun) error
	tracked             map[string]storage.RecoveryRun
	mutex               sync.Mutex
}

// New 创建当前项目的 Crash Recovery Manager。
func New(currentProject *project.Project, database *sql.DB) *Manager {
	return &Manager{
		project: currentProject, database: database, store: storage.NewRunStore(database),
		matchesProcess: processidentity.Matches, recoverFinalization: runner.RecoverFinalization,
		terminateAgent: processidentity.TerminateGroup,
		tracked:        make(map[string]storage.RecoveryRun),
	}
}

// Start 同步处理已死亡 Runner，再后台观察仍存活的旧 Runner；不会调用 Agent 或自动重试。
func (manager *Manager) Start(ctx context.Context) error {
	items, err := manager.store.ListRecoveryRuns(ctx)
	if err != nil {
		return err
	}
	for _, item := range items {
		alive, reconcileErr := manager.reconcile(ctx, item)
		if reconcileErr != nil {
			return reconcileErr
		}
		if alive {
			manager.tracked[item.Run.ID] = item
		}
	}
	if len(manager.tracked) > 0 {
		go manager.monitor(ctx)
	}
	return nil
}

func (manager *Manager) reconcile(ctx context.Context, item storage.RecoveryRun) (bool, error) {
	if item.RunnerPID > 0 && manager.matchesProcess(ctx, item.RunnerPID, item.RunnerIdentity) {
		return true, nil
	}
	cleanupErr := manager.cleanupAgentResources(ctx, item)
	if item.Run.Status == run.StatusFinalizing {
		if err := manager.recoverFinalization(ctx, manager.project, manager.database, item); err == nil {
			return false, nil
		} else if !errors.Is(err, storage.ErrRunStateConflict) {
			return false, manager.markLost(ctx, item, "FINALIZATION_RECOVERY_FAILED", err.Error())
		}
	}
	message := "无法确认旧 Runner 存活；为避免重复调用 AI，Run 已停止并等待人工处理"
	if cleanupErr != nil {
		message += "；Agent 资源未能确认关闭：" + cleanupErr.Error()
	}
	return false, manager.markLost(ctx, item, "RUNNER_LOST", message)
}

func (manager *Manager) cleanupAgentResources(ctx context.Context, item storage.RecoveryRun) error {
	cleanupErr := error(nil)
	if item.AgentPID > 0 {
		cleanupErr = manager.terminateAgent(ctx, item.AgentPID, item.AgentIdentity)
	}
	reason := "旧 Runner 已退出，Agent 进程组已关闭"
	if cleanupErr != nil {
		reason = "旧 Runner 已退出，但 Agent 进程组身份或关闭状态无法确认"
	}
	if err := storage.NewMCPAuditStore(manager.database).ReleaseRunResources(ctx, item.Run.ID, cleanupErr != nil, reason); err != nil {
		return errors.Join(cleanupErr, err)
	}
	return cleanupErr
}

func (manager *Manager) markLost(ctx context.Context, item storage.RecoveryRun, code, message string) error {
	if item.Run.TaskID != "" {
		_, _ = gitworkflow.New(manager.project, manager.database).TaskWorkspace(ctx, item.Run.TaskID)
	}
	_, err := manager.store.RecoverLost(ctx, item.Run.ID, item.Run.LeaseGeneration, code, message)
	return err
}

func (manager *Manager) monitor(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			manager.reconcileTracked(ctx)
		}
	}
}

func (manager *Manager) reconcileTracked(ctx context.Context) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	for id, tracked := range manager.tracked {
		current, err := manager.store.Get(ctx, id)
		if err != nil || !isActive(current.Status) {
			delete(manager.tracked, id)
			continue
		}
		tracked.Run = current
		alive, reconcileErr := manager.reconcile(ctx, tracked)
		if reconcileErr != nil || !alive {
			delete(manager.tracked, id)
		}
	}
}

func isActive(status run.Status) bool {
	return status == run.StatusClaimed || status == run.StatusStarting ||
		status == run.StatusRunning || status == run.StatusFinalizing
}
