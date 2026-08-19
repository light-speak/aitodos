// Package release 定义绑定不可变 Git Commit 的本地发布记录。
package release

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

const (
	maxReleaseTasks       = 200
	maxReleaseVersionSize = 100
)

var semanticVersionPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-((?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*))?(?:\+([0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*))?$`)

// Status 表示本地 Git Tag 创建流程的持久状态。
type Status string

const (
	StatusCreating Status = "CREATING"
	StatusTagged   Status = "TAGGED"
	StatusFailed   Status = "FAILED"
)

// Release 保存语义版本、来源分支和最终 Commit 的不可变对应关系。
type Release struct {
	ID             string     `json:"id"`
	Version        string     `json:"version"`
	TagName        string     `json:"tag_name"`
	SourceBranch   string     `json:"source_branch"`
	CommitSHA      string     `json:"commit_sha"`
	Status         Status     `json:"status"`
	FailureMessage string     `json:"failure_message,omitempty"`
	TaskIDs        []string   `json:"task_ids"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	TaggedAt       *time.Time `json:"tagged_at,omitempty"`
}

// CreateInput 是创建本地 Release 和 annotated tag 所需参数。
type CreateInput struct {
	Version      string   `json:"version"`
	SourceBranch string   `json:"source_branch"`
	TaskIDs      []string `json:"task_ids"`
}

// Normalized 规范化版本、分支和 Task 引用。
func (input CreateInput) Normalized() CreateInput {
	input.Version = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input.Version), "v"))
	input.SourceBranch = strings.TrimSpace(input.SourceBranch)
	input.TaskIDs = uniqueNonEmpty(input.TaskIDs)
	return input
}

// Validate 校验 SemVer 和发布边界。
func (input CreateInput) Validate() error {
	input = input.Normalized()
	if len(input.Version) > maxReleaseVersionSize || !semanticVersionPattern.MatchString(input.Version) {
		return errors.New("release version must be valid semantic version")
	}
	if input.SourceBranch == "" {
		return errors.New("release source branch is required")
	}
	if len(input.TaskIDs) > maxReleaseTasks {
		return errors.New("release must not include more than 200 tasks")
	}
	return nil
}

// TagName 返回发布对应的规范 Git Tag 名称。
func (input CreateInput) TagName() string {
	return "v" + input.Normalized().Version
}

func uniqueNonEmpty(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
