package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/domain/retrievaleval"
	"github.com/light-speak/aitodos/internal/domain/search"
	"github.com/light-speak/aitodos/internal/domain/task"
)

func TestRetrievalEvalStoreRunsProductionSearchAndPreservesHistory(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	tasks := NewTaskStore(database)
	first, err := tasks.Create(ctx, task.CreateInput{Title: "恢复 Worktree", Description: "进程崩溃后的恢复策略"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := tasks.Create(ctx, task.CreateInput{Title: "隔离 Worktree", Description: "每个任务使用独立工作区"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewRetrievalEvalStore(database, NewSearchStore(database))
	created, err := store.AddRelevant(ctx, retrievaleval.CreateCaseInput{
		Query: "Worktree", Kinds: []search.Kind{search.KindTask}, OnlyCurrent: true,
		DocumentID: "TASK:" + first.ID, Note: "恢复场景",
	})
	if err != nil {
		t.Fatal(err)
	}
	created, err = store.AddRelevant(ctx, retrievaleval.CreateCaseInput{
		Query: "Worktree", Kinds: []search.Kind{search.KindTask}, OnlyCurrent: true,
		DocumentID: "TASK:" + second.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(created.Relevances) != 2 {
		t.Fatalf("relevances = %#v", created.Relevances)
	}
	created, err = store.AddRelevant(ctx, retrievaleval.CreateCaseInput{
		Query: "Worktree", Kinds: []search.Kind{search.KindTask}, OnlyCurrent: true,
		DocumentID: "TASK:" + second.ID,
	})
	if err != nil || len(created.Relevances) != 2 {
		t.Fatalf("idempotent relevance = %#v, err = %v", created.Relevances, err)
	}

	run, err := store.Run(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if run.Engine != retrievaleval.EngineLexicalV1 || run.CaseCount != 1 || run.RelevantCount != 2 || run.RecalledCount != 2 || run.RecallAtK != 1 || run.HitAtK != 1 || run.MRR <= 0 {
		t.Fatalf("run = %#v", run)
	}
	if len(run.Results) != 2 || run.Results[0].Rank < 1 || run.Results[1].Rank < 1 {
		t.Fatalf("results = %#v", run.Results)
	}
	if _, err := database.ExecContext(ctx, "DELETE FROM tasks WHERE id = ?", second.ID); err != nil {
		t.Fatal(err)
	}
	staleCases, err := store.ListCases(ctx)
	if err != nil || len(staleCases) != 1 {
		t.Fatalf("stale relevance = %#v, err = %v", staleCases, err)
	}
	foundStale := false
	for _, relevance := range staleCases[0].Relevances {
		if relevance.DocumentID == "TASK:"+second.ID && relevance.Available {
			t.Fatalf("deleted document remains available: %#v", relevance)
		}
		if relevance.DocumentID == "TASK:"+second.ID {
			foundStale = true
		}
	}
	if !foundStale {
		t.Fatal("deleted expected document was not preserved in evaluation case")
	}
	staleRun, err := store.Run(ctx, 10)
	if err != nil || staleRun.RecallAtK != 0.5 || staleRun.HitAtK != 1 {
		t.Fatalf("stale run = %#v, err = %v", staleRun, err)
	}

	if err := store.RemoveRelevant(ctx, created.ID, "TASK:"+first.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.RemoveRelevant(ctx, created.ID, "TASK:"+second.ID); err != nil {
		t.Fatal(err)
	}
	cases, err := store.ListCases(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 0 {
		t.Fatalf("active cases = %#v", cases)
	}
	runs, err := store.ListRuns(ctx, 10)
	if err != nil || len(runs) != 2 || len(runs[0].Results) != 0 {
		t.Fatalf("historical runs = %#v, err = %v", runs, err)
	}
	detail, err := store.GetRun(ctx, run.ID)
	if err != nil || len(detail.Results) != 2 {
		t.Fatalf("run detail = %#v, err = %v", detail, err)
	}
}

func TestRetrievalEvalStoreRejectsUnknownProjectionDocument(t *testing.T) {
	database := openTaskTestDatabase(t)
	store := NewRetrievalEvalStore(database, NewSearchStore(database))
	_, err := store.AddRelevant(context.Background(), retrievaleval.CreateCaseInput{Query: "missing", DocumentID: "TASK:missing"})
	if err != ErrSearchDocumentNotFound {
		t.Fatalf("error = %v, want ErrSearchDocumentNotFound", err)
	}
	if _, err := store.Run(context.Background(), 0); err == nil {
		t.Fatal("expected invalid K to fail")
	}
	if _, err := store.Run(context.Background(), 10); err != ErrRetrievalEvalSuiteEmpty {
		t.Fatalf("empty suite error = %v", err)
	}
	if _, err := store.ListRuns(context.Background(), 0); err == nil {
		t.Fatal("expected invalid history limit to fail")
	}
	if _, err := store.ListRuns(context.Background(), 101); err == nil {
		t.Fatal("expected oversized history limit to fail")
	}
	if err := store.RemoveRelevant(context.Background(), "missing", "TASK:missing"); err != ErrRetrievalEvalCaseNotFound {
		t.Fatalf("missing relevance error = %v", err)
	}
	if _, err := store.GetRun(context.Background(), "missing"); err != ErrRetrievalEvalRunNotFound {
		t.Fatalf("missing run error = %v", err)
	}
}

func TestRetrievalEvalStoreRejectsInvalidInputAndCorruptRows(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	store := NewRetrievalEvalStore(database, NewSearchStore(database))
	if _, err := store.AddRelevant(ctx, retrievaleval.CreateCaseInput{}); err == nil {
		t.Fatal("AddRelevant() accepted invalid input")
	}
	if _, err := store.getCase(ctx, "missing"); !errors.Is(err, ErrRetrievalEvalCaseNotFound) {
		t.Fatalf("missing case error = %v", err)
	}
	now := formatTime(time.Now())
	if _, err := database.ExecContext(ctx, `INSERT INTO retrieval_eval_cases(
id, query, kinds_json, only_current, note, active, created_at, updated_at
) VALUES ('corrupt-case', 'query', '{}', 1, '', 1, ?, ?)`, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListCases(ctx); err == nil {
		t.Fatal("ListCases() accepted non-array kinds")
	}
	if _, err := database.ExecContext(ctx, `UPDATE retrieval_eval_cases SET kinds_json = '[]', created_at = 'invalid' WHERE id = 'corrupt-case'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.getCase(ctx, "corrupt-case"); err == nil {
		t.Fatal("getCase() accepted invalid creation time")
	}
	if _, err := database.ExecContext(ctx, `UPDATE retrieval_eval_cases SET created_at = ?, updated_at = 'invalid' WHERE id = 'corrupt-case'`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.getCase(ctx, "corrupt-case"); err == nil {
		t.Fatal("getCase() accepted invalid update time")
	}
	if _, err := database.ExecContext(ctx, `UPDATE retrieval_eval_cases SET active = 0 WHERE id = 'corrupt-case'`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO retrieval_eval_runs(
id, engine, k, case_count, relevant_count, recalled_count, hit_cases, recall_at_k, hit_at_k, mrr, created_at
) VALUES ('corrupt-run', 'LEXICAL_V1', 10, 1, 1, 1, 1, 1, 1, 1, 'invalid')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetRun(ctx, "corrupt-run"); err == nil {
		t.Fatal("GetRun() accepted invalid creation time")
	}
	if _, err := store.ListRuns(ctx, 10); err == nil {
		t.Fatal("ListRuns() accepted invalid creation time")
	}
	if err := store.insertRun(ctx, retrievaleval.Run{
		ID: "invalid-results", Engine: retrievaleval.EngineLexicalV1, K: 10, CreatedAt: time.Now(),
		Results: []retrievaleval.Result{{CaseID: "missing", DocumentID: "missing", Rank: 1}},
	}); err == nil {
		t.Fatal("insertRun() accepted results without a case")
	}
}

func TestRetrievalEvalStoreReportsClosedDatabase(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	store := NewRetrievalEvalStore(database, NewSearchStore(database))
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	valid := retrievaleval.CreateCaseInput{Query: "query", DocumentID: "TASK:document"}
	calls := []func() error{
		func() error { _, err := store.AddRelevant(ctx, valid); return err },
		func() error { return store.RemoveRelevant(ctx, "case", "document") },
		func() error { _, err := store.ListCases(ctx); return err },
		func() error { _, err := store.getCase(ctx, "case"); return err },
		func() error { _, err := store.Run(ctx, 10); return err },
		func() error { _, err := store.ListRuns(ctx, 10); return err },
		func() error { _, err := store.GetRun(ctx, "run"); return err },
	}
	for index, call := range calls {
		if err := call(); err == nil {
			t.Fatalf("closed call %d unexpectedly succeeded", index)
		}
	}
}
