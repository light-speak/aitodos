// Package scheduler 按项目级 Worker 配置领取 Task 并启动独立 Runner 进程。
package scheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	domainrun "github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/project"
	"github.com/light-speak/aitodos/internal/storage"
)

type launchClaim struct {
	RunID           string
	ClaimToken      string
	LeaseGeneration int64
}

// Scheduler 只调度当前项目，不创建跨项目全局队列。
type Scheduler struct {
	project    *project.Project
	runs       *storage.RunStore
	executable string
	execError  error
	launch     func(context.Context, launchClaim) error
}

// New 创建当前项目 Scheduler。
func New(currentProject *project.Project, database *sql.DB) *Scheduler {
	executable, err := os.Executable()
	scheduler := &Scheduler{
		project: currentProject, runs: storage.NewRunStore(database),
		executable: executable, execError: err,
	}
	scheduler.launch = scheduler.launchRunner
	return scheduler
}

// Run 持续响应 Worker 配置，直到 Daemon 上下文取消。
func (scheduler *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		_ = scheduler.DispatchOnce(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// DispatchOnce 至多领取并启动一个 Run。
func (scheduler *Scheduler) DispatchOnce(ctx context.Context) error {
	settings := scheduler.project.WorkerSettings()
	if !settings.Enabled {
		return nil
	}
	claim, err := scheduler.runs.ClaimNextTask(ctx, settings.MaxWorkers, time.Minute)
	if errors.Is(err, storage.ErrNoRunnableTask) || errors.Is(err, storage.ErrRunCapacityReached) {
		return nil
	}
	if err != nil {
		return err
	}
	launch := launchClaim{
		RunID: claim.Run.ID, ClaimToken: claim.ClaimToken,
		LeaseGeneration: claim.Run.LeaseGeneration,
	}
	if err := scheduler.launch(ctx, launch); err != nil {
		scheduler.failUnstartedRun(launch, "RUNNER_SPAWN_FAILED", err)
		return err
	}
	return nil
}

func (scheduler *Scheduler) launchRunner(_ context.Context, claim launchClaim) error {
	if scheduler.execError != nil {
		return scheduler.execError
	}
	claimReader, claimWriter, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("create runner claim pipe: %w", err)
	}
	defer claimReader.Close()
	defer claimWriter.Close()
	command := exec.Command(scheduler.executable,
		"runner", "--project", scheduler.project.Root, "--run", claim.RunID,
	)
	command.Dir = scheduler.project.Root
	command.ExtraFiles = []*os.File{claimReader}
	command.Env = runnerEnvironment(claim.LeaseGeneration)
	command.Stdout = os.Stderr
	command.Stderr = os.Stderr
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start runner process: %w", err)
	}
	if err := claimReader.Close(); err != nil {
		return fmt.Errorf("close runner claim reader: %w", err)
	}
	if _, err := claimWriter.WriteString(claim.ClaimToken + "\n"); err != nil {
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		_ = command.Wait()
		return fmt.Errorf("send runner claim token: %w", err)
	}
	if err := claimWriter.Close(); err != nil {
		return fmt.Errorf("close runner claim writer: %w", err)
	}
	go scheduler.waitAndReconcile(command, claim)
	return nil
}

func (scheduler *Scheduler) waitAndReconcile(command *exec.Cmd, claim launchClaim) {
	waitErr := command.Wait()
	current, err := scheduler.runs.Get(context.Background(), claim.RunID)
	if err != nil || !activeRunStatus(current.Status) {
		return
	}
	message := "Runner 进程在 Finalization 前退出"
	if waitErr != nil {
		message = waitErr.Error()
	}
	scheduler.failUnstartedRun(claim, "RUNNER_LOST", errors.New(message))
}

func (scheduler *Scheduler) failUnstartedRun(claim launchClaim, code string, cause error) {
	retryable := false
	_, _ = scheduler.runs.Finish(context.Background(), claim.RunID, claim.ClaimToken, claim.LeaseGeneration, storage.RunFinish{
		Status: domainrun.StatusLost, ExitCode: -1, FailureKind: "INFRASTRUCTURE",
		FailureCode: code, FailureMessage: cause.Error(), FailureRetryable: &retryable,
	})
}

func activeRunStatus(status domainrun.Status) bool {
	return status == domainrun.StatusClaimed || status == domainrun.StatusStarting ||
		status == domainrun.StatusRunning || status == domainrun.StatusFinalizing
}

func runnerEnvironment(leaseGeneration int64) []string {
	environment := make([]string, 0, len(os.Environ())+2)
	for _, variable := range os.Environ() {
		name, _, _ := strings.Cut(variable, "=")
		if name != "ATS_CLAIM_TOKEN" && name != "ATS_CLAIM_FD" && name != "ATS_LEASE_GENERATION" {
			environment = append(environment, variable)
		}
	}
	return append(environment,
		"ATS_CLAIM_FD=3",
		"ATS_LEASE_GENERATION="+strconv.FormatInt(leaseGeneration, 10),
	)
}
