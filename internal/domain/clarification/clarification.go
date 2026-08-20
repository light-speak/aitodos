// Package clarification 定义 Agent 阻塞问题和不可覆盖的人工回答。
package clarification

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	domainrun "github.com/light-speak/aitodos/internal/domain/run"
)

// Category 表示阻塞问题的业务类别。
type Category string

const (
	CategoryRequirement Category = "REQUIREMENT"
	CategoryDecision    Category = "DECISION"
	CategoryEnvironment Category = "ENVIRONMENT"
	CategoryValidation  Category = "VALIDATION"
)

// Status 表示 Clarification 是否仍等待人工回答。
type Status string

const (
	StatusOpen     Status = "OPEN"
	StatusAnswered Status = "ANSWERED"
)

// Option 是 Agent 提供的稳定可选答案。
type Option struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// Request 是 Agent 可提交的结构化阻塞问题。
type Request struct {
	Category            Category `json:"category"`
	Question            string   `json:"question"`
	Options             []Option `json:"options"`
	RecommendedOptionID string   `json:"recommended_option_id"`
	AllowCustomAnswer   bool     `json:"allow_custom_answer"`
}

// Normalized 清理问题和选项文本。
func (request Request) Normalized() Request {
	request.Question = strings.TrimSpace(request.Question)
	request.RecommendedOptionID = strings.TrimSpace(request.RecommendedOptionID)
	options := make([]Option, 0, len(request.Options))
	for _, option := range request.Options {
		options = append(options, Option{
			ID: strings.TrimSpace(option.ID), Label: strings.TrimSpace(option.Label),
			Description: strings.TrimSpace(option.Description),
		})
	}
	request.Options = options
	return request
}

// Validate 校验问题可被人类明确回答。
func (request Request) Validate() error {
	request = request.Normalized()
	if !validCategory(request.Category) {
		return errors.New("clarification category 无效")
	}
	if utf8.RuneCountInString(request.Question) < 1 || utf8.RuneCountInString(request.Question) > 4000 {
		return errors.New("clarification question 长度必须为 1 到 4000")
	}
	if len(request.Options) > 6 || (len(request.Options) < 2 && !request.AllowCustomAnswer) {
		return errors.New("clarification 必须包含 2 到 6 个选项，或允许自定义回答")
	}
	seen := make(map[string]struct{}, len(request.Options))
	for _, option := range request.Options {
		if utf8.RuneCountInString(option.ID) < 1 || utf8.RuneCountInString(option.ID) > 64 ||
			utf8.RuneCountInString(option.Label) < 1 || utf8.RuneCountInString(option.Label) > 200 ||
			utf8.RuneCountInString(option.Description) > 1000 {
			return errors.New("clarification option 字段无效")
		}
		if _, exists := seen[option.ID]; exists {
			return errors.New("clarification option id 不得重复")
		}
		seen[option.ID] = struct{}{}
	}
	if request.RecommendedOptionID != "" {
		if _, exists := seen[request.RecommendedOptionID]; !exists {
			return errors.New("recommended_option_id 必须引用已有选项")
		}
	}
	return nil
}

// Clarification 是持久化的 Agent 阻塞问题及人工答案。
type Clarification struct {
	ID                  string            `json:"id"`
	TaskID              string            `json:"task_id"`
	SourceRunID         string            `json:"source_run_id"`
	ContinuationRunID   string            `json:"continuation_run_id,omitempty"`
	ContinuationPurpose domainrun.Purpose `json:"continuation_purpose"`
	Category            Category          `json:"category"`
	Question            string            `json:"question"`
	Options             []Option          `json:"options"`
	RecommendedOptionID string            `json:"recommended_option_id,omitempty"`
	AllowCustomAnswer   bool              `json:"allow_custom_answer"`
	Status              Status            `json:"status"`
	SelectedOptionID    string            `json:"selected_option_id,omitempty"`
	CustomAnswer        string            `json:"custom_answer,omitempty"`
	Version             int64             `json:"version"`
	CreatedAt           time.Time         `json:"created_at"`
	UpdatedAt           time.Time         `json:"updated_at"`
	AnsweredAt          time.Time         `json:"answered_at,omitempty"`
}

// AnswerInput 是回答 Clarification 的领域命令输入。
type AnswerInput struct {
	SelectedOptionID string `json:"selected_option_id"`
	CustomAnswer     string `json:"custom_answer"`
	ExpectedVersion  int64  `json:"expected_version"`
}

// Normalized 清理人工答案。
func (input AnswerInput) Normalized() AnswerInput {
	input.SelectedOptionID = strings.TrimSpace(input.SelectedOptionID)
	input.CustomAnswer = strings.TrimSpace(input.CustomAnswer)
	return input
}

// ValidateFor 校验答案只能选择已有选项或提交允许的自定义文本。
func (input AnswerInput) ValidateFor(item Clarification) error {
	input = input.Normalized()
	if input.SelectedOptionID != "" && input.CustomAnswer != "" {
		return errors.New("不能同时选择选项和填写自定义回答")
	}
	if input.SelectedOptionID != "" {
		for _, option := range item.Options {
			if option.ID == input.SelectedOptionID {
				return nil
			}
		}
		return errors.New("selected_option_id 不存在")
	}
	if !item.AllowCustomAnswer || utf8.RuneCountInString(input.CustomAnswer) < 1 || utf8.RuneCountInString(input.CustomAnswer) > 4000 {
		return errors.New("自定义回答不可用或长度无效")
	}
	return nil
}

func validCategory(category Category) bool {
	return category == CategoryRequirement || category == CategoryDecision ||
		category == CategoryEnvironment || category == CategoryValidation
}
