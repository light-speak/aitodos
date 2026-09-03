package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/light-speak/aitodos/internal/domain/retrievaleval"
	"github.com/light-speak/aitodos/internal/domain/search"
)

var (
	// ErrSearchDocumentNotFound 表示相关性标注引用的搜索文档不存在。
	ErrSearchDocumentNotFound = errors.New("search document not found")
	// ErrRetrievalEvalCaseNotFound 表示检索评测用例不存在。
	ErrRetrievalEvalCaseNotFound = errors.New("retrieval eval case not found")
	// ErrRetrievalEvalRunNotFound 表示检索评测运行不存在。
	ErrRetrievalEvalRunNotFound = errors.New("retrieval eval run not found")
	// ErrRetrievalEvalSuiteEmpty 表示没有可运行的活跃评测用例。
	ErrRetrievalEvalSuiteEmpty = errors.New("retrieval eval suite is empty")
)

// RetrievalEvalStore 维护项目本地的检索评测集和不可变结果。
type RetrievalEvalStore struct {
	database *sql.DB
	search   *SearchStore
}

// NewRetrievalEvalStore 创建检索评测存储。
func NewRetrievalEvalStore(database *sql.DB, searchStore *SearchStore) *RetrievalEvalStore {
	return &RetrievalEvalStore{database: database, search: searchStore}
}

// AddRelevant 幂等创建用例或为已有用例补充相关结果。
func (store *RetrievalEvalStore) AddRelevant(ctx context.Context, input retrievaleval.CreateCaseInput) (retrievaleval.Case, error) {
	input = input.Normalized()
	if err := input.Validate(); err != nil {
		return retrievaleval.Case{}, err
	}
	if err := requireSearchDocument(ctx, store.database, input.DocumentID); err != nil {
		return retrievaleval.Case{}, err
	}
	kindsJSON, err := json.Marshal(input.Kinds)
	if err != nil {
		return retrievaleval.Case{}, fmt.Errorf("encode retrieval eval kinds: %w", err)
	}
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return retrievaleval.Case{}, fmt.Errorf("begin retrieval eval update: %w", err)
	}
	defer transaction.Rollback()
	caseID, err := upsertRetrievalEvalCase(ctx, transaction, input, string(kindsJSON))
	if err != nil {
		return retrievaleval.Case{}, err
	}
	if _, err := transaction.ExecContext(ctx, `INSERT INTO retrieval_eval_relevances(case_id, document_id, created_at)
VALUES (?, ?, ?) ON CONFLICT(case_id, document_id) DO NOTHING`, caseID, input.DocumentID, formatTime(time.Now().UTC())); err != nil {
		return retrievaleval.Case{}, fmt.Errorf("insert retrieval relevance: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return retrievaleval.Case{}, fmt.Errorf("commit retrieval eval update: %w", err)
	}
	return store.getCase(ctx, caseID)
}

func upsertRetrievalEvalCase(ctx context.Context, transaction *sql.Tx, input retrievaleval.CreateCaseInput, kindsJSON string) (string, error) {
	id, err := newID()
	if err != nil {
		return "", err
	}
	now := formatTime(time.Now().UTC())
	_, err = transaction.ExecContext(ctx, `INSERT INTO retrieval_eval_cases(
id, query, kinds_json, only_current, note, active, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, 1, ?, ?)
ON CONFLICT(query, kinds_json, only_current) DO UPDATE SET
note = CASE WHEN excluded.note = '' THEN retrieval_eval_cases.note ELSE excluded.note END,
active = 1, updated_at = excluded.updated_at`, id, input.Query, kindsJSON, input.OnlyCurrent, input.Note, now, now)
	if err != nil {
		return "", fmt.Errorf("upsert retrieval eval case: %w", err)
	}
	if err := transaction.QueryRowContext(ctx, `SELECT id FROM retrieval_eval_cases
WHERE query = ? AND kinds_json = ? AND only_current = ?`, input.Query, kindsJSON, input.OnlyCurrent).Scan(&id); err != nil {
		return "", fmt.Errorf("read retrieval eval case id: %w", err)
	}
	return id, nil
}

func requireSearchDocument(ctx context.Context, database *sql.DB, documentID string) error {
	var exists int
	if err := database.QueryRowContext(ctx, "SELECT 1 FROM search_documents WHERE id = ?", documentID).Scan(&exists); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrSearchDocumentNotFound
		}
		return fmt.Errorf("read search document: %w", err)
	}
	return nil
}

// RemoveRelevant 移除一条相关性标注；用例无剩余标注时自动停用。
func (store *RetrievalEvalStore) RemoveRelevant(ctx context.Context, caseID, documentID string) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin retrieval relevance removal: %w", err)
	}
	defer transaction.Rollback()
	result, err := transaction.ExecContext(ctx, `DELETE FROM retrieval_eval_relevances WHERE case_id = ? AND document_id = ?`, caseID, documentID)
	if err != nil {
		return fmt.Errorf("delete retrieval relevance: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrRetrievalEvalCaseNotFound
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE retrieval_eval_cases SET active = 0, updated_at = ?
WHERE id = ? AND NOT EXISTS (SELECT 1 FROM retrieval_eval_relevances WHERE case_id = ?)`, formatTime(time.Now().UTC()), caseID, caseID); err != nil {
		return fmt.Errorf("deactivate empty retrieval eval case: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit retrieval relevance removal: %w", err)
	}
	return nil
}

// ListCases 返回全部活跃评测用例。
func (store *RetrievalEvalStore) ListCases(ctx context.Context) ([]retrievaleval.Case, error) {
	rows, err := store.database.QueryContext(ctx, `SELECT id, query, kinds_json, only_current, note, active, created_at, updated_at
FROM retrieval_eval_cases WHERE active = 1 ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, fmt.Errorf("list retrieval eval cases: %w", err)
	}
	defer rows.Close()
	cases := make([]retrievaleval.Case, 0)
	for rows.Next() {
		item, scanErr := scanRetrievalEvalCase(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		cases = append(cases, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate retrieval eval cases: %w", err)
	}
	if err := store.loadRelevances(ctx, cases); err != nil {
		return nil, err
	}
	return cases, nil
}

func (store *RetrievalEvalStore) getCase(ctx context.Context, caseID string) (retrievaleval.Case, error) {
	row := store.database.QueryRowContext(ctx, `SELECT id, query, kinds_json, only_current, note, active, created_at, updated_at
FROM retrieval_eval_cases WHERE id = ?`, caseID)
	item, err := scanRetrievalEvalCase(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return retrievaleval.Case{}, ErrRetrievalEvalCaseNotFound
		}
		return retrievaleval.Case{}, err
	}
	items := []retrievaleval.Case{item}
	if err := store.loadRelevances(ctx, items); err != nil {
		return retrievaleval.Case{}, err
	}
	return items[0], nil
}

func scanRetrievalEvalCase(row rowScanner) (retrievaleval.Case, error) {
	var item retrievaleval.Case
	var kindsJSON, createdAt, updatedAt string
	if err := row.Scan(&item.ID, &item.Query, &kindsJSON, &item.OnlyCurrent, &item.Note, &item.Active, &createdAt, &updatedAt); err != nil {
		return item, err
	}
	if err := json.Unmarshal([]byte(kindsJSON), &item.Kinds); err != nil {
		return item, fmt.Errorf("decode retrieval eval kinds: %w", err)
	}
	var err error
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return item, err
	}
	item.UpdatedAt, err = parseTime(updatedAt)
	return item, err
}

func (store *RetrievalEvalStore) loadRelevances(ctx context.Context, cases []retrievaleval.Case) error {
	for index := range cases {
		rows, err := store.database.QueryContext(ctx, `SELECT relevance.document_id,
COALESCE(document.stable_key, ''), COALESCE(document.title, ''), document.id IS NOT NULL
FROM retrieval_eval_relevances AS relevance
LEFT JOIN search_documents AS document ON document.id = relevance.document_id
WHERE relevance.case_id = ? ORDER BY relevance.document_id`, cases[index].ID)
		if err != nil {
			return fmt.Errorf("list retrieval relevances: %w", err)
		}
		for rows.Next() {
			var relevance retrievaleval.Relevance
			if err := rows.Scan(&relevance.DocumentID, &relevance.StableKey, &relevance.Title, &relevance.Available); err != nil {
				rows.Close()
				return fmt.Errorf("scan retrieval relevance: %w", err)
			}
			cases[index].Relevances = append(cases[index].Relevances, relevance)
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	return nil
}

// Run 通过生产 SearchStore 执行全部活跃用例并保存不可变结果。
func (store *RetrievalEvalStore) Run(ctx context.Context, k int) (retrievaleval.Run, error) {
	if k < 1 || k > 50 {
		return retrievaleval.Run{}, errors.New("评测 K 必须为 1 到 50")
	}
	cases, err := store.ListCases(ctx)
	if err != nil {
		return retrievaleval.Run{}, err
	}
	if len(cases) == 0 {
		return retrievaleval.Run{}, ErrRetrievalEvalSuiteEmpty
	}
	results, rankings, err := store.rankCases(ctx, cases, k)
	if err != nil {
		return retrievaleval.Run{}, err
	}
	metrics := retrievaleval.CalculateMetrics(rankings)
	id, err := newID()
	if err != nil {
		return retrievaleval.Run{}, err
	}
	run := retrievaleval.Run{
		ID: id, Engine: retrievaleval.EngineLexicalV1, K: k, CaseCount: metrics.CaseCount,
		RelevantCount: metrics.RelevantCount, RecalledCount: metrics.RecalledCount, HitCases: metrics.HitCases,
		RecallAtK: metrics.RecallAtK, HitAtK: metrics.HitAtK, MRR: metrics.MRR,
		Results: results, CreatedAt: time.Now().UTC(),
	}
	if err := store.insertRun(ctx, run); err != nil {
		return retrievaleval.Run{}, err
	}
	return run, nil
}

func (store *RetrievalEvalStore) rankCases(ctx context.Context, cases []retrievaleval.Case, k int) ([]retrievaleval.Result, []retrievaleval.CaseRanking, error) {
	results := make([]retrievaleval.Result, 0)
	rankings := make([]retrievaleval.CaseRanking, 0, len(cases))
	for _, item := range cases {
		page, err := store.search.Search(ctx, search.Query{Text: item.Query, Kinds: item.Kinds, OnlyCurrent: item.OnlyCurrent, Limit: k})
		if err != nil {
			return nil, nil, fmt.Errorf("evaluate retrieval case %s: %w", item.ID, err)
		}
		ranks := make(map[string]int, len(page.Items))
		for index, result := range page.Items {
			ranks[result.DocumentID] = index + 1
		}
		ranking := retrievaleval.CaseRanking{RelevantCount: len(item.Relevances)}
		for _, relevance := range item.Relevances {
			rank := ranks[relevance.DocumentID]
			ranking.Ranks = append(ranking.Ranks, rank)
			results = append(results, retrievaleval.Result{CaseID: item.ID, DocumentID: relevance.DocumentID, Rank: rank})
		}
		rankings = append(rankings, ranking)
	}
	return results, rankings, nil
}

func (store *RetrievalEvalStore) insertRun(ctx context.Context, run retrievaleval.Run) error {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin retrieval eval run: %w", err)
	}
	defer transaction.Rollback()
	_, err = transaction.ExecContext(ctx, `INSERT INTO retrieval_eval_runs(
id, engine, k, case_count, relevant_count, recalled_count, hit_cases, recall_at_k, hit_at_k, mrr, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, run.ID, run.Engine, run.K, run.CaseCount, run.RelevantCount,
		run.RecalledCount, run.HitCases, run.RecallAtK, run.HitAtK, run.MRR, formatTime(run.CreatedAt))
	if err != nil {
		return fmt.Errorf("insert retrieval eval run: %w", err)
	}
	for _, result := range run.Results {
		var rank any
		if result.Rank > 0 {
			rank = result.Rank
		}
		if _, err := transaction.ExecContext(ctx, `INSERT INTO retrieval_eval_results(run_id, case_id, document_id, rank)
VALUES (?, ?, ?, ?)`, run.ID, result.CaseID, result.DocumentID, rank); err != nil {
			return fmt.Errorf("insert retrieval eval result: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit retrieval eval run: %w", err)
	}
	return nil
}

// ListRuns 返回最近的不可变评测结果。
func (store *RetrievalEvalStore) ListRuns(ctx context.Context, limit int) ([]retrievaleval.Run, error) {
	if limit < 1 || limit > 100 {
		return nil, errors.New("评测历史数量必须为 1 到 100")
	}
	rows, err := store.database.QueryContext(ctx, `SELECT id, engine, k, case_count, relevant_count,
recalled_count, hit_cases, recall_at_k, hit_at_k, mrr, created_at
FROM retrieval_eval_runs ORDER BY created_at DESC, id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list retrieval eval runs: %w", err)
	}
	defer rows.Close()
	runs := make([]retrievaleval.Run, 0)
	for rows.Next() {
		run, scanErr := scanRetrievalEvalRun(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		runs = append(runs, run)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate retrieval eval runs: %w", err)
	}
	return runs, nil
}

// GetRun 返回一次评测运行及逐相关文档排名。
func (store *RetrievalEvalStore) GetRun(ctx context.Context, runID string) (retrievaleval.Run, error) {
	row := store.database.QueryRowContext(ctx, `SELECT id, engine, k, case_count, relevant_count,
recalled_count, hit_cases, recall_at_k, hit_at_k, mrr, created_at
FROM retrieval_eval_runs WHERE id = ?`, runID)
	run, err := scanRetrievalEvalRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return retrievaleval.Run{}, ErrRetrievalEvalRunNotFound
		}
		return retrievaleval.Run{}, err
	}
	run.Results, err = store.listRunResults(ctx, run.ID)
	return run, err
}

func scanRetrievalEvalRun(row rowScanner) (retrievaleval.Run, error) {
	run := retrievaleval.Run{Results: make([]retrievaleval.Result, 0)}
	var createdAt string
	if err := row.Scan(&run.ID, &run.Engine, &run.K, &run.CaseCount, &run.RelevantCount, &run.RecalledCount,
		&run.HitCases, &run.RecallAtK, &run.HitAtK, &run.MRR, &createdAt); err != nil {
		return run, err
	}
	var err error
	run.CreatedAt, err = parseTime(createdAt)
	return run, err
}

func (store *RetrievalEvalStore) listRunResults(ctx context.Context, runID string) ([]retrievaleval.Result, error) {
	rows, err := store.database.QueryContext(ctx, `SELECT case_id, document_id, COALESCE(rank, 0)
FROM retrieval_eval_results WHERE run_id = ? ORDER BY case_id, document_id`, runID)
	if err != nil {
		return nil, fmt.Errorf("list retrieval eval results: %w", err)
	}
	defer rows.Close()
	results := make([]retrievaleval.Result, 0)
	for rows.Next() {
		var result retrievaleval.Result
		if err := rows.Scan(&result.CaseID, &result.DocumentID, &result.Rank); err != nil {
			return nil, fmt.Errorf("scan retrieval eval result: %w", err)
		}
		results = append(results, result)
	}
	return results, rows.Err()
}
