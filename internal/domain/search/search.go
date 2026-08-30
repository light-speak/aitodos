// Package search 定义项目内统一只读检索模型。
package search

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// Kind 表示可检索规范数据的类型。
type Kind string

const (
	KindTopic         Kind = "TOPIC"
	KindTask          Kind = "TASK"
	KindMessage       Kind = "MESSAGE"
	KindPlanRevision  Kind = "PLAN_REVISION"
	KindClarification Kind = "CLARIFICATION"
)

// Query 是有界全文检索输入。
type Query struct {
	Text          string
	Kinds         []Kind
	Statuses      []string
	OnlyCurrent   bool
	UpdatedAfter  time.Time
	UpdatedBefore time.Time
	Limit         int
	Cursor        string
}

// Normalized 清理查询文本、过滤条件和默认分页大小。
func (query Query) Normalized() Query {
	query.Text = strings.TrimSpace(query.Text)
	query.Cursor = strings.TrimSpace(query.Cursor)
	if query.Limit == 0 {
		query.Limit = 20
	}
	query.Kinds = uniqueKinds(query.Kinds)
	query.Statuses = uniqueStrings(query.Statuses)
	return query
}

// Validate 校验查询规模和过滤值。
func (query Query) Validate() error {
	query = query.Normalized()
	if utf8.RuneCountInString(query.Text) < 1 || utf8.RuneCountInString(query.Text) > 500 {
		return errors.New("搜索文本长度必须为 1 到 500")
	}
	if query.Limit < 1 || query.Limit > 50 {
		return errors.New("搜索结果数必须为 1 到 50")
	}
	if len(query.Kinds) > 5 || len(query.Statuses) > 20 || len(query.Cursor) > 100 {
		return errors.New("搜索过滤条件过多")
	}
	for _, kind := range query.Kinds {
		if !kind.Valid() {
			return errors.New("搜索类型无效")
		}
	}
	for _, status := range query.Statuses {
		if utf8.RuneCountInString(status) > 100 {
			return errors.New("搜索状态无效")
		}
	}
	if !query.UpdatedAfter.IsZero() && !query.UpdatedBefore.IsZero() && query.UpdatedAfter.After(query.UpdatedBefore) {
		return errors.New("搜索更新时间范围无效")
	}
	return nil
}

// Valid 判断检索类型是否受支持。
func (kind Kind) Valid() bool {
	return kind == KindTopic || kind == KindTask || kind == KindMessage ||
		kind == KindPlanRevision || kind == KindClarification
}

// Item 是 Search Projection 返回的稳定检索结果。
type Item struct {
	DocumentID  string    `json:"document_id"`
	Kind        Kind      `json:"kind"`
	SourceID    string    `json:"source_id"`
	SubjectKind string    `json:"subject_kind"`
	SubjectID   string    `json:"subject_id"`
	StableKey   string    `json:"stable_key"`
	Title       string    `json:"title"`
	Snippet     string    `json:"snippet"`
	Status      string    `json:"status"`
	Current     bool      `json:"current"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Page 是有界搜索结果和下一页游标。
type Page struct {
	Items      []Item `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

func uniqueKinds(values []Kind) []Kind {
	result := make([]Kind, 0, len(values))
	seen := make(map[Kind]struct{}, len(values))
	for _, value := range values {
		value = Kind(strings.TrimSpace(string(value)))
		if _, exists := seen[value]; value == "" || exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func uniqueStrings(values []string) []string {
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
