package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/quality"
	"github.com/light-speak/aitodos/internal/domain/task"
)

func TestQualityStoreKeepsEstimateHistoryAndLatestTestEvidence(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	created, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "进度能力"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewQualityStore(database)
	if _, err := store.CreateEstimate(ctx, created.ID, quality.EstimateInput{
		Points: 5, RemainingPoints: 5, Confidence: 0.5, Rationale: "初始估算", Source: quality.EstimateHuman,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateEstimate(ctx, created.ID, quality.EstimateInput{
		Points: 5, RemainingPoints: 3, Confidence: 0.8, Rationale: "接口已完成", Source: quality.EstimateHuman,
	}); err != nil {
		t.Fatal(err)
	}
	testCase, err := store.CreateTestCase(ctx, created.ID, quality.TestCaseInput{
		Title: "运行 Go 测试", Required: true, CreatedBy: quality.TestCreatorHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddTestResult(ctx, created.ID, testCase.ID, quality.TestResultInput{
		Outcome: quality.OutcomePassed, EvidenceKind: quality.EvidenceHuman, Summary: "全部通过",
	}); err != nil {
		t.Fatal(err)
	}

	loaded, err := store.GetTaskQuality(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Estimate == nil || loaded.Estimate.Revision != 2 || loaded.Estimate.RemainingPoints != 3 {
		t.Fatalf("current estimate = %#v", loaded.Estimate)
	}
	if len(loaded.EstimateHistory) != 2 || len(loaded.TestCases) != 1 || loaded.TestCases[0].LatestResult == nil {
		t.Fatalf("task quality = %#v", loaded)
	}
}

func TestRequiredTestEvidenceGatesTaskAcceptance(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	tasks := NewTaskStore(database)
	created, err := tasks.Create(ctx, task.CreateInput{Title: "验收门禁"})
	if err != nil {
		t.Fatal(err)
	}
	qualityStore := NewQualityStore(database)
	testCase, err := qualityStore.CreateTestCase(ctx, created.ID, quality.TestCaseInput{
		Title: "关键回归", Required: true, CreatedBy: quality.TestCreatorHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	review, err := tasks.ApplyCommand(ctx, created.ID, created.Version, task.CommandSubmitReview)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tasks.ApplyReview(ctx, review.ID, review.Version, task.ReviewInput{
		Decision: task.ReviewAccepted, Comment: "通过",
	}, "abc"); !errors.Is(err, ErrRequiredTestsNotPassed) {
		t.Fatalf("accept without evidence error = %v", err)
	}
	if _, err := qualityStore.AddTestResult(ctx, created.ID, testCase.ID, quality.TestResultInput{
		Outcome: quality.OutcomePassed, EvidenceKind: quality.EvidenceHuman, Summary: "人工回归通过",
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := tasks.ApplyReview(ctx, review.ID, review.Version, task.ReviewInput{
		Decision: task.ReviewAccepted, Comment: "通过",
	}, "abc"); err != nil {
		t.Fatal(err)
	}
}

func TestQualityStoreEnsuresRequiredTestsPassed(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	created, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "验证公开门禁"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewQualityStore(database)
	testCase, err := store.CreateTestCase(ctx, created.ID, quality.TestCaseInput{
		Title: "关键回归", Required: true, CreatedBy: quality.TestCreatorHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureRequiredTestsPassed(ctx, created.ID); !errors.Is(err, ErrRequiredTestsNotPassed) {
		t.Fatalf("missing evidence error = %v", err)
	}
	if _, err := store.AddTestResult(ctx, created.ID, testCase.ID, quality.TestResultInput{
		Outcome: quality.OutcomePassed, EvidenceKind: quality.EvidenceHuman, Summary: "人工确认通过",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureRequiredTestsPassed(ctx, created.ID); err != nil {
		t.Fatalf("verified evidence error = %v", err)
	}
}

func TestLegacyCommandWithoutExitCodeDoesNotPassVerification(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	created, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "旧版测试证据"})
	if err != nil {
		t.Fatal(err)
	}
	testCase, err := NewQualityStore(database).CreateTestCase(ctx, created.ID, quality.TestCaseInput{
		Title: "旧版命令", Required: true, CreatedBy: quality.TestCreatorHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO task_test_results(
id, test_case_id, task_id, outcome, evidence_kind, summary, command, artifact_ref, source_run_id, created_at, exit_code
) VALUES ('legacy-result', ?, ?, 'PASSED', 'COMMAND', '旧记录', 'go test ./...', '', NULL, '2026-01-01T00:00:00Z', NULL)`,
		testCase.ID, created.ID); err != nil {
		t.Fatal(err)
	}
	progress, err := NewQualityStore(database).ProjectProgress(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if progress.VerifiedPassedTests != 0 {
		t.Fatalf("legacy command counted as verified: %#v", progress)
	}
	review, err := NewTaskStore(database).ApplyCommand(ctx, created.ID, created.Version, task.CommandSubmitReview)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewTaskStore(database).ApplyReview(ctx, review.ID, review.Version, task.ReviewInput{
		Decision: task.ReviewAccepted, Comment: "通过",
	}, "abc"); !errors.Is(err, ErrRequiredTestsNotPassed) {
		t.Fatalf("legacy command acceptance error = %v", err)
	}
}

func TestQualityStoreBuildsExplainableProjectProgress(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	tasks := NewTaskStore(database)
	first, _ := tasks.Create(ctx, task.CreateInput{Title: "第一个"})
	second, _ := tasks.Create(ctx, task.CreateInput{Title: "第二个"})
	store := NewQualityStore(database)
	_, _ = store.CreateEstimate(ctx, first.ID, quality.EstimateInput{
		Points: 5, RemainingPoints: 2, Confidence: 0.8, Rationale: "完成三点", Source: quality.EstimateHuman,
	})
	_, _ = store.CreateEstimate(ctx, second.ID, quality.EstimateInput{
		Points: 3, RemainingPoints: 3, Confidence: 0.6, Rationale: "尚未开始", Source: quality.EstimateHuman,
	})
	review, _ := tasks.ApplyCommand(ctx, first.ID, first.Version, task.CommandSubmitReview)
	_, _, _ = tasks.ApplyReview(ctx, review.ID, review.Version, task.ReviewInput{
		Decision: task.ReviewAccepted, Comment: "通过",
	}, "abc")

	progress, err := store.ProjectProgress(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if progress.TotalTasks != 2 || progress.AcceptedTasks != 1 || progress.StrictPercent != 50 {
		t.Fatalf("strict progress = %#v", progress)
	}
	if progress.TotalPoints != 8 || progress.RemainingPoints != 3 || progress.ForecastPercent == nil || *progress.ForecastPercent != 62.5 {
		t.Fatalf("forecast progress = %#v", progress)
	}
}
