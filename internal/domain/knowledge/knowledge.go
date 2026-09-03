// Package knowledge 定义可检索的决策、标签、执行摘要和 CI 检查快照。
package knowledge

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// DecisionStatus 表示决策是否仍然有效。
type DecisionStatus string

const (
	DecisionStatusActive     DecisionStatus = "ACTIVE"
	DecisionStatusSuperseded DecisionStatus = "SUPERSEDED"
)

// Decision 保存绑定到一个 Topic 或 Task 的不可变决策。
type Decision struct {
	ID                   string         `json:"id"`
	Key                  string         `json:"key"`
	TopicID              string         `json:"topic_id,omitempty"`
	TaskID               string         `json:"task_id,omitempty"`
	Title                string         `json:"title"`
	Content              string         `json:"content"`
	Status               DecisionStatus `json:"status"`
	SupersedesDecisionID string         `json:"supersedes_decision_id,omitempty"`
	CreatedAt            time.Time      `json:"created_at"`
}

// DecisionInput 是创建新决策允许提供的字段。
type DecisionInput struct {
	TopicID              string `json:"topic_id"`
	TaskID               string `json:"task_id"`
	Title                string `json:"title"`
	Content              string `json:"content"`
	SupersedesDecisionID string `json:"supersedes_decision_id"`
}

// Normalized 清理决策输入中的首尾空白。
func (input DecisionInput) Normalized() DecisionInput {
	input.TopicID = strings.TrimSpace(input.TopicID)
	input.TaskID = strings.TrimSpace(input.TaskID)
	input.Title = strings.TrimSpace(input.Title)
	input.Content = strings.TrimSpace(input.Content)
	input.SupersedesDecisionID = strings.TrimSpace(input.SupersedesDecisionID)
	return input
}

// Validate 校验决策必须且只能属于一个主体。
func (input DecisionInput) Validate() error {
	if (input.TopicID == "") == (input.TaskID == "") {
		return errors.New("decision must belong to exactly one topic or task")
	}
	if len(input.Title) == 0 || len(input.Title) > 200 {
		return errors.New("decision title must contain 1 to 200 characters")
	}
	if len(input.Content) == 0 || len(input.Content) > 20000 {
		return errors.New("decision content must contain 1 to 20000 characters")
	}
	return nil
}

// Label 是只影响展示、搜索和筛选的项目标签。
type Label struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color"`
	CreatedAt time.Time `json:"created_at"`
}

// LabelInput 是创建标签允许提供的字段。
type LabelInput struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

var labelColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// Normalized 清理标签输入并补齐默认颜色。
func (input LabelInput) Normalized() LabelInput {
	input.Name = strings.TrimSpace(input.Name)
	input.Color = strings.TrimSpace(input.Color)
	if input.Color == "" {
		input.Color = "#64748B"
	}
	return input
}

// Validate 校验标签名称和颜色。
func (input LabelInput) Validate() error {
	if len(input.Name) == 0 || len(input.Name) > 100 {
		return errors.New("label name must contain 1 to 100 characters")
	}
	if !labelColorPattern.MatchString(input.Color) {
		return errors.New("label color must be a six-digit hexadecimal color")
	}
	return nil
}

// RunSummary 保存可重建的单次 Run 摘要。
type RunSummary struct {
	RunID       string    `json:"run_id"`
	Status      string    `json:"status"`
	Summary     string    `json:"summary"`
	PassedTests int       `json:"passed_tests"`
	FailedTests int       `json:"failed_tests"`
	CreatedAt   time.Time `json:"created_at"`
}

// CICheck 是一次 CI Snapshot 中的单项检查。
type CICheck struct {
	Name       string `json:"name"`
	State      string `json:"state"`
	DetailsURL string `json:"details_url,omitempty"`
}

// CICheckSnapshot 是从外部 CI 系统显式导入的不可变观察值。
type CICheckSnapshot struct {
	ID         string    `json:"id"`
	TaskID     string    `json:"task_id"`
	Provider   string    `json:"provider"`
	CommitSHA  string    `json:"commit_sha"`
	State      string    `json:"state"`
	Checks     []CICheck `json:"checks"`
	SourceURL  string    `json:"source_url,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// CISnapshotInput 是显式导入 CI 观察值允许提供的字段。
type CISnapshotInput struct {
	Provider   string    `json:"provider"`
	CommitSHA  string    `json:"commit_sha"`
	State      string    `json:"state"`
	Checks     []CICheck `json:"checks"`
	SourceURL  string    `json:"source_url"`
	ObservedAt time.Time `json:"observed_at"`
}

// Normalized 清理 CI 输入并标准化枚举值。
func (input CISnapshotInput) Normalized() CISnapshotInput {
	input.Provider = strings.TrimSpace(input.Provider)
	input.CommitSHA = strings.TrimSpace(input.CommitSHA)
	input.State = strings.ToUpper(strings.TrimSpace(input.State))
	input.SourceURL = strings.TrimSpace(input.SourceURL)
	for index := range input.Checks {
		input.Checks[index].Name = strings.TrimSpace(input.Checks[index].Name)
		input.Checks[index].State = strings.ToUpper(strings.TrimSpace(input.Checks[index].State))
		input.Checks[index].DetailsURL = strings.TrimSpace(input.Checks[index].DetailsURL)
	}
	return input
}

// Validate 校验 CI 快照中的稳定身份和状态。
func (input CISnapshotInput) Validate() error {
	if len(input.Provider) == 0 || len(input.Provider) > 100 {
		return errors.New("CI provider must contain 1 to 100 characters")
	}
	if len(input.CommitSHA) < 7 || len(input.CommitSHA) > 64 {
		return errors.New("CI commit SHA must contain 7 to 64 characters")
	}
	if !validCIState(input.State) {
		return errors.New("CI state is invalid")
	}
	if len(input.SourceURL) > 2000 {
		return errors.New("CI source URL is too long")
	}
	for _, check := range input.Checks {
		if check.Name == "" || len(check.Name) > 200 || !validCIState(check.State) || len(check.DetailsURL) > 2000 {
			return errors.New("CI check is invalid")
		}
	}
	return nil
}

func validCIState(state string) bool {
	return state == "PENDING" || state == "PASSED" || state == "FAILED" || state == "CANCELLED" || state == "UNKNOWN"
}
