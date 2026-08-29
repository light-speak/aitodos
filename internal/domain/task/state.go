package task

import "fmt"

// Status 表示 Task 的业务状态，与 Run 状态相互独立。
type Status string

const (
	StatusBacklog          Status = "BACKLOG"
	StatusReady            Status = "READY"
	StatusRunning          Status = "RUNNING"
	StatusReview           Status = "REVIEW"
	StatusAccepted         Status = "ACCEPTED"
	StatusChangesRequested Status = "CHANGES_REQUESTED"
	StatusBlocked          Status = "BLOCKED"
	StatusCancelled        Status = "CANCELLED"
)

// Command 表示可驱动 Task 状态迁移的领域命令。
type Command string

const (
	CommandSubmitReview         Command = "SUBMIT_REVIEW"
	CommandClaimRun             Command = "CLAIM_RUN"
	CommandRunSucceeded         Command = "RUN_SUCCEEDED"
	CommandRunFailed            Command = "RUN_FAILED"
	CommandNeedsInput           Command = "NEEDS_INPUT"
	CommandCancelRun            Command = "CANCEL_RUN"
	CommandAccept               Command = "ACCEPT"
	CommandReject               Command = "REJECT"
	CommandRetry                Command = "RETRY"
	CommandResumeImplementation Command = "RESUME_IMPLEMENTATION"
	CommandResumeRevision       Command = "RESUME_REVISION"
	CommandRequestChanges       Command = "REQUEST_CHANGES"
)

var transitions = map[Status]map[Command]Status{
	StatusReady: {
		CommandClaimRun:       StatusRunning,
		CommandSubmitReview:   StatusReview,
		CommandRequestChanges: StatusChangesRequested,
	},
	StatusRunning: {
		CommandRunSucceeded: StatusReview,
		CommandRunFailed:    StatusBlocked,
		CommandNeedsInput:   StatusBlocked,
		CommandCancelRun:    StatusBlocked,
	},
	StatusReview: {
		CommandAccept:         StatusAccepted,
		CommandReject:         StatusChangesRequested,
		CommandRequestChanges: StatusChangesRequested,
	},
	StatusChangesRequested: {
		CommandClaimRun:       StatusRunning,
		CommandRequestChanges: StatusChangesRequested,
	},
	StatusBlocked: {
		CommandRetry:                StatusReady,
		CommandResumeImplementation: StatusReady,
		CommandResumeRevision:       StatusChangesRequested,
		CommandRequestChanges:       StatusChangesRequested,
	},
}

// TransitionError 表示当前状态不接受指定领域命令。
type TransitionError struct {
	Current Status
	Command Command
}

// Error 返回不包含敏感信息的状态迁移错误。
func (err *TransitionError) Error() string {
	return fmt.Sprintf("task status %q does not allow command %q", err.Current, err.Command)
}

// Transition 根据当前状态和命令计算目标状态。
func Transition(current Status, command Command) (Status, error) {
	if next, ok := transitions[current][command]; ok {
		return next, nil
	}
	return "", &TransitionError{Current: current, Command: command}
}

// AllStatuses 返回状态机定义的全部 Task 状态。
func AllStatuses() []Status {
	return []Status{
		StatusBacklog,
		StatusReady,
		StatusRunning,
		StatusReview,
		StatusAccepted,
		StatusChangesRequested,
		StatusBlocked,
		StatusCancelled,
	}
}

// AllCommands 返回状态机定义的全部领域命令。
func AllCommands() []Command {
	return []Command{
		CommandSubmitReview,
		CommandClaimRun,
		CommandRunSucceeded,
		CommandRunFailed,
		CommandNeedsInput,
		CommandCancelRun,
		CommandAccept,
		CommandReject,
		CommandRetry,
		CommandResumeImplementation,
		CommandResumeRevision,
		CommandRequestChanges,
	}
}
