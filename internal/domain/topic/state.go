package topic

import "fmt"

// Status 表示 Topic 的业务状态。
type Status string

const (
	StatusOpen               Status = "OPEN"
	StatusNeedsClarification Status = "NEEDS_CLARIFICATION"
	StatusPlanReview         Status = "PLAN_REVIEW"
	StatusPlanned            Status = "PLANNED"
	StatusClosed             Status = "CLOSED"
)

// Command 表示可驱动 Topic 状态迁移的领域命令。
type Command string

const (
	CommandRequestClarification Command = "REQUEST_CLARIFICATION"
	CommandAnswerClarification  Command = "ANSWER_CLARIFICATION"
	CommandSubmitPlan           Command = "SUBMIT_PLAN"
	CommandApprovePlan          Command = "APPROVE_PLAN"
	CommandRequestPlanChanges   Command = "REQUEST_PLAN_CHANGES"
	CommandClose                Command = "CLOSE"
)

var transitions = map[Status]map[Command]Status{
	StatusOpen: {
		CommandRequestClarification: StatusNeedsClarification,
		CommandSubmitPlan:           StatusPlanReview,
		CommandClose:                StatusClosed,
	},
	StatusNeedsClarification: {
		CommandAnswerClarification: StatusOpen,
	},
	StatusPlanReview: {
		CommandApprovePlan:        StatusPlanned,
		CommandRequestPlanChanges: StatusOpen,
	},
	StatusPlanned: {
		CommandClose: StatusClosed,
	},
}

// TransitionError 表示当前状态不接受指定领域命令。
type TransitionError struct {
	Current Status
	Command Command
}

// Error 返回状态迁移错误。
func (err *TransitionError) Error() string {
	return fmt.Sprintf("topic status %q does not allow command %q", err.Current, err.Command)
}

// Transition 根据当前状态和命令计算目标状态。
func Transition(current Status, command Command) (Status, error) {
	if next, ok := transitions[current][command]; ok {
		return next, nil
	}
	return "", &TransitionError{Current: current, Command: command}
}

// AllStatuses 返回全部 Topic 状态。
func AllStatuses() []Status {
	return []Status{StatusOpen, StatusNeedsClarification, StatusPlanReview, StatusPlanned, StatusClosed}
}

// AllCommands 返回全部 Topic 命令。
func AllCommands() []Command {
	return []Command{
		CommandRequestClarification,
		CommandAnswerClarification,
		CommandSubmitPlan,
		CommandApprovePlan,
		CommandRequestPlanChanges,
		CommandClose,
	}
}
