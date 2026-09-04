package run

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// StopReason 表示 Agent 为什么在当前边界停止工作。
type StopReason string

const (
	StopReasonGoalReached        StopReason = "GOAL_REACHED"
	StopReasonDiscussionRequired StopReason = "DISCUSSION_REQUIRED"
	StopReasonNeedsInput         StopReason = "NEEDS_INPUT"
	StopReasonEnvironmentBlocked StopReason = "ENVIRONMENT_BLOCKED"
	StopReasonPolicyBlocked      StopReason = "POLICY_BLOCKED"
	StopReasonLimitReached       StopReason = "LIMIT_REACHED"
	StopReasonProcessFailed      StopReason = "PROCESS_FAILED"
	StopReasonCancelled          StopReason = "CANCELLED"
	StopReasonTimedOut           StopReason = "TIMED_OUT"
	StopReasonLost               StopReason = "LOST"
)

// Verification 将完成声明和可检查证据分开保存。
type Verification struct {
	Claim    string `json:"claim"`
	Evidence string `json:"evidence"`
}

// Closure 是一次 Run 的不可变诚实收口报告。
type Closure struct {
	RunID          string         `json:"run_id,omitempty"`
	StopReason     StopReason     `json:"stop_reason"`
	Summary        string         `json:"summary"`
	Completed      []string       `json:"completed"`
	Verified       []Verification `json:"verified"`
	Unverified     []string       `json:"unverified"`
	RemainingRisks []string       `json:"remaining_risks"`
	NextAction     string         `json:"next_action"`
	CreatedAt      time.Time      `json:"created_at,omitempty"`
}

// Normalized 清理报告文本，空列表仍保持空列表。
func (closure Closure) Normalized() Closure {
	closure.RunID = strings.TrimSpace(closure.RunID)
	closure.Summary = strings.TrimSpace(closure.Summary)
	closure.Completed = normalizeClosureStrings(closure.Completed)
	closure.Unverified = normalizeClosureStrings(closure.Unverified)
	closure.RemainingRisks = normalizeClosureStrings(closure.RemainingRisks)
	closure.NextAction = strings.TrimSpace(closure.NextAction)
	for index := range closure.Verified {
		closure.Verified[index].Claim = strings.TrimSpace(closure.Verified[index].Claim)
		closure.Verified[index].Evidence = strings.TrimSpace(closure.Verified[index].Evidence)
	}
	return closure
}

// Validate 校验收口报告既能说明完成内容，也不会隐藏阻塞后的下一步。
func (closure Closure) Validate() error {
	closure = closure.Normalized()
	if !validStopReason(closure.StopReason) {
		return errors.New("run closure stop_reason 无效")
	}
	if closure.Summary == "" || utf8.RuneCountInString(closure.Summary) > 4000 {
		return errors.New("run closure summary 不能为空且最长 4000 个字符")
	}
	if err := validateClosureStrings(closure.Completed, "completed"); err != nil {
		return err
	}
	if err := validateClosureStrings(closure.Unverified, "unverified"); err != nil {
		return err
	}
	if err := validateClosureStrings(closure.RemainingRisks, "remaining_risks"); err != nil {
		return err
	}
	if closure.StopReason == StopReasonGoalReached && len(closure.Completed) == 0 {
		return errors.New("GOAL_REACHED 必须列出已完成内容")
	}
	if closure.StopReason != StopReasonGoalReached && closure.NextAction == "" {
		return errors.New("未完成收口必须提供 next_action")
	}
	if utf8.RuneCountInString(closure.NextAction) > 2000 {
		return errors.New("run closure next_action 最长 2000 个字符")
	}
	if len(closure.Verified) > 50 {
		return errors.New("verified 最多 50 项")
	}
	for _, verification := range closure.Verified {
		if verification.Claim == "" || verification.Evidence == "" || utf8.RuneCountInString(verification.Claim) > 1000 || utf8.RuneCountInString(verification.Evidence) > 2000 {
			return errors.New("verified 声明和证据不能为空且不能超过长度限制")
		}
	}
	return nil
}

// StatusForClosure 将 Agent 主动停止原因映射到可信的 Run 终态。
func StatusForClosure(closure Closure) Status {
	if closure.StopReason == StopReasonGoalReached {
		return StatusSucceeded
	}
	return StatusFailed
}

func validStopReason(reason StopReason) bool {
	switch reason {
	case StopReasonGoalReached, StopReasonDiscussionRequired, StopReasonNeedsInput,
		StopReasonEnvironmentBlocked, StopReasonPolicyBlocked, StopReasonLimitReached,
		StopReasonProcessFailed, StopReasonCancelled, StopReasonTimedOut, StopReasonLost:
		return true
	default:
		return false
	}
}

func normalizeClosureStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if normalized := strings.TrimSpace(value); normalized != "" {
			result = append(result, normalized)
		}
	}
	return result
}

func validateClosureStrings(values []string, field string) error {
	if len(values) > 50 {
		return errors.New(field + " 最多 50 项")
	}
	for _, value := range values {
		if utf8.RuneCountInString(value) > 1000 {
			return errors.New(field + " 单项最长 1000 个字符")
		}
	}
	return nil
}
