// Package experience 定义可验证、可追溯的项目经验与动态召回评分。
package experience

import (
	"errors"
	"math"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// Status 表示经验当前是否可用于 Run Context。
type Status string

const (
	StatusCandidate  Status = "CANDIDATE"
	StatusActive     Status = "ACTIVE"
	StatusChallenged Status = "CHALLENGED"
	StatusSuperseded Status = "SUPERSEDED"
)

// Outcome 表示一次召回后的人工或 Agent 反馈。
type Outcome string

const (
	OutcomePending Outcome = "PENDING"
	OutcomeHelpful Outcome = "HELPFUL"
	OutcomeHarmful Outcome = "HARMFUL"
	OutcomeIgnored Outcome = "IGNORED"
)

// Record 是由 Topic 或 Task 产生、可选择项目范围复用的经验。
type Record struct {
	ID                     string    `json:"id"`
	Key                    string    `json:"key"`
	TopicID                string    `json:"topic_id,omitempty"`
	TaskID                 string    `json:"task_id,omitempty"`
	Title                  string    `json:"title"`
	Summary                string    `json:"summary"`
	Guidance               string    `json:"guidance"`
	Applicability          string    `json:"applicability"`
	ProjectWide            bool      `json:"project_wide"`
	Status                 Status    `json:"status"`
	Pinned                 bool      `json:"pinned"`
	VerificationCount      int       `json:"verification_count"`
	SuccessfulApplications int       `json:"successful_applications"`
	FailedApplications     int       `json:"failed_applications"`
	RecallCount            int       `json:"recall_count"`
	SourceRunID            string    `json:"source_run_id,omitempty"`
	SupersedesExperienceID string    `json:"supersedes_experience_id,omitempty"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// Input 是创建经验允许提供的字段。HTTP 中的人类创建会直接形成已验证的 ACTIVE 记录。
type Input struct {
	TopicID                string `json:"topic_id"`
	TaskID                 string `json:"task_id"`
	Title                  string `json:"title"`
	Summary                string `json:"summary"`
	Guidance               string `json:"guidance"`
	Applicability          string `json:"applicability"`
	ProjectWide            bool   `json:"project_wide"`
	Pinned                 bool   `json:"pinned"`
	SourceRunID            string `json:"source_run_id"`
	SupersedesExperienceID string `json:"supersedes_experience_id"`
}

// Normalized 清理输入空白。
func (input Input) Normalized() Input {
	input.TopicID = strings.TrimSpace(input.TopicID)
	input.TaskID = strings.TrimSpace(input.TaskID)
	input.Title = strings.TrimSpace(input.Title)
	input.Summary = strings.TrimSpace(input.Summary)
	input.Guidance = strings.TrimSpace(input.Guidance)
	input.Applicability = strings.TrimSpace(input.Applicability)
	input.SourceRunID = strings.TrimSpace(input.SourceRunID)
	input.SupersedesExperienceID = strings.TrimSpace(input.SupersedesExperienceID)
	return input
}

// Validate 校验经验来源主体和有界文本。
func (input Input) Validate() error {
	input = input.Normalized()
	if (input.TopicID == "") == (input.TaskID == "") {
		return errors.New("experience must belong to exactly one topic or task")
	}
	if !validLength(input.Title, 1, 200) || !validLength(input.Summary, 1, 1000) ||
		!validLength(input.Guidance, 1, 4000) || !validLength(input.Applicability, 1, 1000) {
		return errors.New("experience text is invalid")
	}
	return nil
}

func validLength(value string, minimum, maximum int) bool {
	length := utf8.RuneCountInString(value)
	return length >= minimum && length <= maximum
}

// InputSignals 是一次召回的可解释评分输入；召回次数不参与评分。
type InputSignals struct {
	Relevance         float64
	ScopeMatch        float64
	Freshness         float64
	VerificationCount int
	SuccessCount      int
	FailureCount      int
	Pinned            bool
}

// ScoreBreakdown 保存动态召回的评分分量。
type ScoreBreakdown struct {
	Relevance float64 `json:"relevance_score"`
	Utility   float64 `json:"utility_score"`
	Scope     float64 `json:"scope_score"`
	Freshness float64 `json:"freshness_score"`
	Final     float64 `json:"final_score"`
}

// Score 使用相关性、证据、范围、时效和人工固定计算动态分数。
func Score(input InputSignals) ScoreBreakdown {
	relevance := clamp(input.Relevance)
	scope := clamp(input.ScopeMatch)
	freshness := clamp(input.Freshness)
	evidence := 1 - math.Exp(-float64(input.VerificationCount)/2)
	applications := input.SuccessCount + input.FailureCount
	successRate := 0.5
	if applications > 0 {
		successRate = float64(input.SuccessCount+1) / float64(applications+2)
	}
	utility := clamp(0.45*evidence + 0.55*successRate)
	pinBonus := 0.0
	if input.Pinned {
		pinBonus = 0.05
	}
	final := clamp(0.5*relevance + 0.25*utility + 0.15*scope + 0.05*freshness + pinBonus)
	return ScoreBreakdown{Relevance: relevance, Utility: utility, Scope: scope, Freshness: freshness, Final: final}
}

// LexicalRelevance 使用 Unicode 单词和相邻字符片段计算无需 Embedding 的相关性。
func LexicalRelevance(query string, values ...string) float64 {
	queryTokens := tokens(query)
	candidateTokens := tokens(strings.Join(values, " "))
	if len(queryTokens) == 0 || len(candidateTokens) == 0 {
		return 0
	}
	matched := 0
	for token := range queryTokens {
		if _, exists := candidateTokens[token]; exists {
			matched++
		}
	}
	return clamp(float64(matched) / math.Sqrt(float64(len(queryTokens)*len(candidateTokens))))
}

func tokens(value string) map[string]struct{} {
	value = strings.ToLower(value)
	result := make(map[string]struct{})
	var word []rune
	flush := func() {
		if len(word) == 0 {
			return
		}
		result[string(word)] = struct{}{}
		for index := 0; index+1 < len(word); index++ {
			result[string(word[index:index+2])] = struct{}{}
		}
		word = word[:0]
	}
	for _, current := range value {
		if unicode.IsLetter(current) || unicode.IsNumber(current) || current == '_' || current == '-' {
			word = append(word, current)
			continue
		}
		flush()
	}
	flush()
	return result
}

func clamp(value float64) float64 {
	return math.Max(0, math.Min(1, value))
}
