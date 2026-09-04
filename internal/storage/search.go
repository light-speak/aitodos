package storage

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/light-speak/aitodos/internal/domain/search"
)

const maxSearchOffset = 10000

// ErrInvalidSearchCursor 表示分页游标无法安全解析。
var ErrInvalidSearchCursor = errors.New("搜索游标无效")

// SearchStore 维护可重建全文投影并提供项目内统一读取能力。
type SearchStore struct {
	database *sql.DB
}

// NewSearchStore 创建 Search Projection 读取服务。
func NewSearchStore(database *sql.DB) *SearchStore {
	return &SearchStore{database: database}
}

// Search 使用 FTS5 和结构化过滤返回有界结果。
func (store *SearchStore) Search(ctx context.Context, input search.Query) (search.Page, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return search.Page{}, err
	}
	offset, err := decodeSearchCursor(input.Cursor)
	if err != nil {
		return search.Page{}, err
	}
	statement, arguments := buildSearchStatement(input, offset)
	rows, err := store.database.QueryContext(ctx, statement, arguments...)
	if err != nil {
		return search.Page{}, fmt.Errorf("search projection: %w", err)
	}
	defer rows.Close()
	page := search.Page{Items: make([]search.Item, 0, input.Limit)}
	for rows.Next() {
		item, scanErr := scanSearchItem(rows)
		if scanErr != nil {
			return search.Page{}, scanErr
		}
		if len(page.Items) == input.Limit {
			page.NextCursor = encodeSearchCursor(offset + input.Limit)
			break
		}
		page.Items = append(page.Items, item)
	}
	if err := rows.Err(); err != nil {
		return search.Page{}, fmt.Errorf("iterate search results: %w", err)
	}
	return page, nil
}

// Rebuild 从规范表原子重建全部 Search Projection。
func (store *SearchStore) Rebuild(ctx context.Context) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin search rebuild: %w", err)
	}
	defer transaction.Rollback()
	if _, err := transaction.ExecContext(ctx, "DELETE FROM search_documents"); err != nil {
		return fmt.Errorf("clear search projection: %w", err)
	}
	for _, statement := range searchRebuildStatements {
		if _, err := transaction.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("rebuild search projection: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit search rebuild: %w", err)
	}
	return nil
}

func buildSearchStatement(input search.Query, offset int) (string, []any) {
	where, arguments, useFTS := searchPredicates(input)
	from := "search_documents AS documents"
	snippet := "substr(documents.body, 1, 240)"
	order := "documents.updated_at DESC, documents.id ASC"
	if useFTS {
		from = "search_documents_fts JOIN search_documents AS documents ON documents.rowid = search_documents_fts.rowid"
		snippet = "snippet(search_documents_fts, 1, '', '', '…', 32)"
		order = "bm25(search_documents_fts, 8.0, 1.0, 12.0), documents.updated_at DESC, documents.id ASC"
	}
	statement := `SELECT documents.id, documents.kind, documents.source_id,
       documents.subject_kind, documents.subject_id, documents.stable_key,
       documents.title, ` + snippet + `, documents.status,
       documents.is_current, documents.updated_at
FROM ` + from + ` WHERE ` + strings.Join(where, " AND ") + `
ORDER BY ` + order + ` LIMIT ? OFFSET ?`
	arguments = append(arguments, input.Limit+1, offset)
	return statement, arguments
}

func searchPredicates(input search.Query) ([]string, []any, bool) {
	terms := strings.Fields(input.Text)
	useFTS := len(terms) > 0
	for _, term := range terms {
		if utf8.RuneCountInString(term) < 3 {
			useFTS = false
			break
		}
	}
	where := make([]string, 0, 4)
	arguments := make([]any, 0, 8)
	if useFTS {
		where = append(where, "search_documents_fts MATCH ?")
		arguments = append(arguments, quotedFTSQuery(terms))
	} else {
		for _, term := range terms {
			where = append(where, `(documents.title LIKE ? ESCAPE '\' OR documents.body LIKE ? ESCAPE '\' OR documents.stable_key LIKE ? ESCAPE '\')`)
			pattern := "%" + escapeLike(term) + "%"
			arguments = append(arguments, pattern, pattern, pattern)
		}
	}
	where, arguments = appendSearchFilter(where, arguments, "documents.kind", kindsToStrings(input.Kinds))
	where, arguments = appendSearchFilter(where, arguments, "documents.status", input.Statuses)
	if input.OnlyCurrent {
		where = append(where, "documents.is_current = 1")
	}
	if !input.UpdatedAfter.IsZero() {
		where = append(where, "documents.updated_at >= ?")
		arguments = append(arguments, formatTime(input.UpdatedAfter.UTC()))
	}
	if !input.UpdatedBefore.IsZero() {
		where = append(where, "documents.updated_at <= ?")
		arguments = append(arguments, formatTime(input.UpdatedBefore.UTC()))
	}
	return where, arguments, useFTS
}

func appendSearchFilter(where []string, arguments []any, column string, values []string) ([]string, []any) {
	if len(values) == 0 {
		return where, arguments
	}
	placeholders := make([]string, len(values))
	for index, value := range values {
		placeholders[index] = "?"
		arguments = append(arguments, value)
	}
	return append(where, column+" IN ("+strings.Join(placeholders, ", ")+")"), arguments
}

func kindsToStrings(kinds []search.Kind) []string {
	values := make([]string, len(kinds))
	for index, kind := range kinds {
		values[index] = string(kind)
	}
	return values
}

func quotedFTSQuery(terms []string) string {
	quoted := make([]string, len(terms))
	for index, term := range terms {
		quoted[index] = `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
	}
	return strings.Join(quoted, " AND ")
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	return strings.ReplaceAll(value, `_`, `\_`)
}

func scanSearchItem(scanner rowScanner) (search.Item, error) {
	var item search.Item
	var current int
	var updatedAt string
	if err := scanner.Scan(
		&item.DocumentID, &item.Kind, &item.SourceID, &item.SubjectKind,
		&item.SubjectID, &item.StableKey, &item.Title, &item.Snippet,
		&item.Status, &current, &updatedAt,
	); err != nil {
		return search.Item{}, fmt.Errorf("scan search result: %w", err)
	}
	parsed, err := parseTime(updatedAt)
	if err != nil {
		return search.Item{}, err
	}
	item.Current = current != 0
	item.UpdatedAt = parsed
	return item, nil
}

func encodeSearchCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeSearchCursor(cursor string) (int, error) {
	if cursor == "" {
		return 0, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, ErrInvalidSearchCursor
	}
	offset, err := strconv.Atoi(string(decoded))
	if err != nil || offset < 0 || offset > maxSearchOffset {
		return 0, ErrInvalidSearchCursor
	}
	return offset, nil
}

var searchRebuildStatements = []string{
	`INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'TOPIC:' || id, 'TOPIC', id, 'TOPIC', id, topic_key, title, description, status, 1, updated_at FROM topics`,
	`INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'TASK:' || id, 'TASK', id, 'TASK', id, task_key, title,
       description || char(10) || acceptance_criteria, status, 1, updated_at FROM tasks`,
	`INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'MESSAGE:' || messages.id, 'MESSAGE', messages.id,
       CASE WHEN threads.topic_id IS NOT NULL THEN 'TOPIC' ELSE 'TASK' END,
       COALESCE(threads.topic_id, threads.task_id),
       COALESCE(topics.topic_key, tasks.task_key) || '#message-' || messages.sequence,
       CASE messages.author_kind WHEN 'HUMAN' THEN '你的消息' WHEN 'AGENT' THEN 'Agent 消息' ELSE '系统消息' END,
       messages.content, messages.author_kind, 1, messages.created_at
FROM messages JOIN threads ON threads.id = messages.thread_id
LEFT JOIN topics ON topics.id = threads.topic_id LEFT JOIN tasks ON tasks.id = threads.task_id`,
	`INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'PLAN_REVISION:' || revisions.id, 'PLAN_REVISION', revisions.id, 'TOPIC', plans.topic_id,
       plans.plan_key || '-R' || revisions.revision_number,
       plans.plan_key || ' Revision ' || revisions.revision_number,
       revisions.summary || char(10) || revisions.rationale || char(10) || revisions.risks || char(10) ||
       COALESCE((SELECT group_concat(drafts.title || char(10) || drafts.description || char(10) || drafts.acceptance_criteria, char(10))
                 FROM plan_task_drafts AS drafts WHERE drafts.plan_revision_id = revisions.id), ''),
       plans.status, CASE WHEN plans.current_revision_id = revisions.id THEN 1 ELSE 0 END, plans.updated_at
FROM plan_revisions AS revisions JOIN plans ON plans.id = revisions.plan_id`,
	`INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'CLARIFICATION:' || clarifications.id, 'CLARIFICATION', clarifications.id,
       CASE WHEN clarifications.topic_id IS NOT NULL THEN 'TOPIC' ELSE 'TASK' END,
       COALESCE(clarifications.topic_id, clarifications.task_id),
       COALESCE(topics.topic_key, tasks.task_key) || '#clarification-' || clarifications.id,
       COALESCE(topics.topic_key, tasks.task_key) || ' · ' || clarifications.category,
       clarifications.question || char(10) || clarifications.options_json || char(10) ||
       clarifications.selected_option_id || char(10) || clarifications.custom_answer,
       clarifications.status, 1, clarifications.updated_at
FROM clarifications LEFT JOIN topics ON topics.id = clarifications.topic_id
LEFT JOIN tasks ON tasks.id = clarifications.task_id`,
	`INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'DECISION:' || id, 'DECISION', id,
       CASE WHEN topic_id IS NOT NULL THEN 'TOPIC' ELSE 'TASK' END,
       COALESCE(topic_id, task_id), decision_key, title, content, status,
       CASE WHEN status = 'ACTIVE' THEN 1 ELSE 0 END, created_at FROM decisions`,
	`INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'RUN_SUMMARY:' || summaries.run_id, 'RUN_SUMMARY', summaries.run_id,
       CASE WHEN runs.topic_id IS NOT NULL THEN 'TOPIC' ELSE 'TASK' END,
       COALESCE(runs.topic_id, runs.task_id), runs.id,
       runs.purpose || ' Run 摘要', summaries.summary, summaries.status, 1, summaries.created_at
FROM run_summaries summaries JOIN runs ON runs.id = summaries.run_id`,
	`INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'CI_CHECK:' || snapshots.id, 'CI_CHECK', snapshots.id, 'TASK', snapshots.task_id,
       snapshots.commit_sha, snapshots.provider || ' CI', snapshots.checks_json,
       snapshots.state, 1, snapshots.observed_at FROM ci_check_snapshots snapshots`,
	`INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'LABEL:TOPIC:' || bindings.topic_id || ':' || labels.id, 'LABEL', bindings.topic_id || ':' || labels.id,
       'TOPIC', bindings.topic_id, labels.name, labels.name, labels.color, 'ATTACHED', 1, bindings.created_at
FROM topic_labels bindings JOIN labels ON labels.id = bindings.label_id`,
	`INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'LABEL:TASK:' || bindings.task_id || ':' || labels.id, 'LABEL', bindings.task_id || ':' || labels.id,
       'TASK', bindings.task_id, labels.name, labels.name, labels.color, 'ATTACHED', 1, bindings.created_at
FROM task_labels bindings JOIN labels ON labels.id = bindings.label_id`,
	`INSERT INTO search_documents(id, kind, source_id, subject_kind, subject_id, stable_key, title, body, status, is_current, updated_at)
SELECT 'EXPERIENCE:' || id, 'EXPERIENCE', id,
       CASE WHEN topic_id IS NOT NULL THEN 'TOPIC' ELSE 'TASK' END, COALESCE(topic_id, task_id),
       experience_key, title, summary || char(10) || guidance || char(10) || applicability,
       status, CASE WHEN status = 'ACTIVE' THEN 1 ELSE 0 END, updated_at FROM experience_records`,
}
