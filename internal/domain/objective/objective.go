// Package objective 定义跨 Topic、Plan 和 Task 的持久长期目标。
package objective

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Status 表示长期目标的人工生命周期。
type Status string

const (
	StatusActive    Status = "ACTIVE"
	StatusPaused    Status = "PAUSED"
	StatusAchieved  Status = "ACHIEVED"
	StatusCancelled Status = "CANCELLED"
)

// Command 表示允许执行的显式生命周期命令。
type Command string

const (
	CommandPause   Command = "PAUSE"
	CommandResume  Command = "RESUME"
	CommandAchieve Command = "ACHIEVE"
	CommandCancel  Command = "CANCEL"
)

// Criterion 是一个必须可验证的完成条件。
type Criterion struct {
	ID          string `json:"id"`
	Description string `json:"description"`
}

// Objective 保存长期目标当前指针和并发版本。
type Objective struct {
	ID                string     `json:"id"`
	Key               string     `json:"key"`
	RootTopicID       string     `json:"root_topic_id"`
	Status            Status     `json:"status"`
	CurrentRevisionID string     `json:"current_revision_id"`
	MaxContinuations  int        `json:"max_continuations"`
	ContinuationCount int        `json:"continuation_count"`
	Version           int64      `json:"version"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
}

// Revision 保存不可变目标说明和完成条件。
type Revision struct {
	ID                 string      `json:"id"`
	ObjectiveID        string      `json:"objective_id"`
	Revision           int         `json:"revision"`
	Statement          string      `json:"statement"`
	Scope              string      `json:"scope"`
	Constraints        []string    `json:"constraints"`
	CompletionCriteria []Criterion `json:"completion_criteria"`
	PreviousRevisionID string      `json:"previous_revision_id,omitempty"`
	CreatedAt          time.Time   `json:"created_at"`
}

// Progress 是由完成条件和根 Topic 关联 Task 推导的进度。
type Progress struct {
	CriteriaTotal     int `json:"criteria_total"`
	CriteriaSatisfied int `json:"criteria_satisfied"`
	TasksTotal        int `json:"tasks_total"`
	TasksAccepted     int `json:"tasks_accepted"`
}

// View 聚合长期目标当前 Revision、最近 Checkpoint 和派生进度。
type View struct {
	Objective        Objective   `json:"objective"`
	Revision         Revision    `json:"revision"`
	LatestCheckpoint *Checkpoint `json:"latest_checkpoint,omitempty"`
	Progress         Progress    `json:"progress"`
}

// CreateInput 是创建长期目标时允许提供的字段。
type CreateInput struct {
	RootTopicID        string   `json:"root_topic_id"`
	Statement          string   `json:"statement"`
	Scope              string   `json:"scope"`
	Constraints        []string `json:"constraints"`
	CompletionCriteria []string `json:"completion_criteria"`
}

// Normalized 清理文本、去除空项和重复项。
func (input CreateInput) Normalized() CreateInput {
	input.RootTopicID = strings.TrimSpace(input.RootTopicID)
	input.Statement = strings.TrimSpace(input.Statement)
	input.Scope = strings.TrimSpace(input.Scope)
	input.Constraints = normalizeUnique(input.Constraints)
	input.CompletionCriteria = normalizeUnique(input.CompletionCriteria)
	return input
}

// Validate 校验长期目标创建输入。
func (input CreateInput) Validate() error {
	input = input.Normalized()
	if input.RootTopicID == "" || input.Statement == "" {
		return errors.New("根 Topic 和目标说明不能为空")
	}
	if utf8.RuneCountInString(input.Statement) > 4000 || utf8.RuneCountInString(input.Scope) > 4000 {
		return errors.New("目标说明或范围过长")
	}
	if len(input.Constraints) > 20 || len(input.CompletionCriteria) < 1 || len(input.CompletionCriteria) > 50 {
		return errors.New("约束最多 20 项，完成条件必须为 1 到 50 项")
	}
	return validateTextItems(input.Constraints, input.CompletionCriteria)
}

// Transition 根据当前状态和显式命令计算目标状态。
func Transition(status Status, command Command) (Status, error) {
	transitions := map[Status]map[Command]Status{
		StatusActive: {CommandPause: StatusPaused, CommandAchieve: StatusAchieved, CommandCancel: StatusCancelled},
		StatusPaused: {CommandResume: StatusActive, CommandCancel: StatusCancelled},
	}
	if next, ok := transitions[status][command]; ok {
		return next, nil
	}
	return "", fmt.Errorf("不能从 %s 执行 %s", status, command)
}

func normalizeUnique(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if _, exists := seen[value]; value == "" || exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func validateTextItems(groups ...[]string) error {
	for _, values := range groups {
		for _, value := range values {
			if utf8.RuneCountInString(value) > 1000 {
				return errors.New("约束或完成条件单项不能超过 1000 字符")
			}
		}
	}
	return nil
}
