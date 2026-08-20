package task

import (
	"errors"
	"strings"
	"time"
)

const maxReviewCommentLength = 5000

// ReviewDecision 表示人工验收结论。
type ReviewDecision string

const (
	ReviewAccepted ReviewDecision = "ACCEPTED"
	ReviewRejected ReviewDecision = "REJECTED"
)

// Review 保存一次不可变的人工验收记录。
type Review struct {
	ID        string         `json:"id"`
	TaskID    string         `json:"task_id"`
	Decision  ReviewDecision `json:"decision"`
	Comment   string         `json:"comment"`
	CommitSHA string         `json:"commit_sha,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
}

// ReviewInput 是人工验收命令的输入。
type ReviewInput struct {
	Decision ReviewDecision `json:"decision"`
	Comment  string         `json:"comment"`
}

// Normalized 规范化人工验收文本。
func (input ReviewInput) Normalized() ReviewInput {
	input.Comment = strings.TrimSpace(input.Comment)
	return input
}

// Validate 校验验收结论和必填原因。
func (input ReviewInput) Validate() error {
	input = input.Normalized()
	if input.Decision != ReviewAccepted && input.Decision != ReviewRejected {
		return errors.New("review decision is invalid")
	}
	if input.Decision == ReviewRejected && input.Comment == "" {
		return errors.New("rejected review comment is required")
	}
	if len(input.Comment) > maxReviewCommentLength {
		return errors.New("review comment is too long")
	}
	return nil
}

// Command 返回验收结论对应的 Task 状态命令。
func (input ReviewInput) Command() Command {
	if input.Decision == ReviewAccepted {
		return CommandAccept
	}
	return CommandReject
}
