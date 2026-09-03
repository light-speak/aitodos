// Package retrievaleval 定义项目检索质量评测模型。
package retrievaleval

import (
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/light-speak/aitodos/internal/domain/search"
)

// EngineLexicalV1 标识当前 SQLite FTS/LIKE 检索实现。
const EngineLexicalV1 = "LEXICAL_V1"

// CreateCaseInput 表示从真实搜索结果创建或补充评测用例的输入。
type CreateCaseInput struct {
	Query       string        `json:"query"`
	Kinds       []search.Kind `json:"kinds"`
	OnlyCurrent bool          `json:"only_current"`
	DocumentID  string        `json:"document_id"`
	Note        string        `json:"note"`
}

// Normalized 返回稳定、可去重的用例输入。
func (input CreateCaseInput) Normalized() CreateCaseInput {
	input.Query = strings.TrimSpace(input.Query)
	input.DocumentID = strings.TrimSpace(input.DocumentID)
	input.Note = strings.TrimSpace(input.Note)
	seen := make(map[search.Kind]struct{}, len(input.Kinds))
	kinds := make([]search.Kind, 0, len(input.Kinds))
	for _, kind := range input.Kinds {
		if _, exists := seen[kind]; kind == "" || exists {
			continue
		}
		seen[kind] = struct{}{}
		kinds = append(kinds, kind)
	}
	sort.Slice(kinds, func(left, right int) bool { return kinds[left] < kinds[right] })
	input.Kinds = kinds
	return input
}

// Validate 校验评测用例不会绕过生产搜索约束。
func (input CreateCaseInput) Validate() error {
	input = input.Normalized()
	query := search.Query{Text: input.Query, Kinds: input.Kinds, OnlyCurrent: input.OnlyCurrent, Limit: 10}
	if err := query.Validate(); err != nil {
		return err
	}
	if utf8.RuneCountInString(input.DocumentID) < 3 || utf8.RuneCountInString(input.DocumentID) > 300 {
		return errors.New("相关结果标识长度必须为 3 到 300")
	}
	if utf8.RuneCountInString(input.Note) > 1000 {
		return errors.New("评测备注不能超过 1000 字")
	}
	return nil
}

// Relevance 是一个人工标记的相关结果。
type Relevance struct {
	DocumentID string `json:"document_id"`
	StableKey  string `json:"stable_key"`
	Title      string `json:"title"`
	Available  bool   `json:"available"`
}

// Case 是一个查询及其人工相关性判断。
type Case struct {
	ID          string        `json:"id"`
	Query       string        `json:"query"`
	Kinds       []search.Kind `json:"kinds"`
	OnlyCurrent bool          `json:"only_current"`
	Note        string        `json:"note"`
	Active      bool          `json:"active"`
	Relevances  []Relevance   `json:"relevances"`
	CreatedAt   time.Time     `json:"created_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

// Result 保存一次运行中单个相关结果的实际排名，0 表示未召回。
type Result struct {
	CaseID     string `json:"case_id"`
	DocumentID string `json:"document_id"`
	Rank       int    `json:"rank"`
}

// Run 保存可比较的检索评测快照。
type Run struct {
	ID            string    `json:"id"`
	Engine        string    `json:"engine"`
	K             int       `json:"k"`
	CaseCount     int       `json:"case_count"`
	RelevantCount int       `json:"relevant_count"`
	RecalledCount int       `json:"recalled_count"`
	HitCases      int       `json:"hit_cases"`
	RecallAtK     float64   `json:"recall_at_k"`
	HitAtK        float64   `json:"hit_at_k"`
	MRR           float64   `json:"mrr"`
	Results       []Result  `json:"results"`
	CreatedAt     time.Time `json:"created_at"`
}

// CaseRanking 是单个评测用例的相关文档排名。
type CaseRanking struct {
	RelevantCount int
	Ranks         []int
}

// Metrics 是跨用例聚合的微平均召回与按用例平均的命中率、MRR。
type Metrics struct {
	CaseCount     int
	RelevantCount int
	RecalledCount int
	HitCases      int
	RecallAtK     float64
	HitAtK        float64
	MRR           float64
}

// CalculateMetrics 从排名计算 Recall@K、Hit@K 与 MRR。
func CalculateMetrics(rankings []CaseRanking) Metrics {
	metrics := Metrics{CaseCount: len(rankings)}
	for _, ranking := range rankings {
		metrics.RelevantCount += ranking.RelevantCount
		firstRank := 0
		for _, rank := range ranking.Ranks {
			if rank <= 0 {
				continue
			}
			metrics.RecalledCount++
			if firstRank == 0 || rank < firstRank {
				firstRank = rank
			}
		}
		if firstRank > 0 {
			metrics.HitCases++
			metrics.MRR += 1 / float64(firstRank)
		}
	}
	if metrics.RelevantCount > 0 {
		metrics.RecallAtK = float64(metrics.RecalledCount) / float64(metrics.RelevantCount)
	}
	if metrics.CaseCount > 0 {
		metrics.HitAtK = float64(metrics.HitCases) / float64(metrics.CaseCount)
		metrics.MRR /= float64(metrics.CaseCount)
	}
	return metrics
}
