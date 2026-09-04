package storage

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/domain/agentprofile"
	"github.com/light-speak/aitodos/internal/domain/assessment"
	"github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/task"
)

func TestAssessmentStoreCreatesRevisionAndAppliesUnlockedAITitle(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureProfile(t, database, agentprofile.RoleTriager)
	tasks := NewTaskStore(database)
	created, err := tasks.Create(ctx, task.CreateInput{Description: "我想让搜索支持状态和时间组合筛选"})
	if err != nil {
		t.Fatal(err)
	}
	claim := startTriageRun(t, database)

	stored, updated, err := NewAssessmentStore(database).ApplyTriageResult(ctx, claim.Run.ID, assessment.Input{
		SuggestedTitle: "实现搜索组合筛选",
		Scores: assessment.DimensionScores{
			TechnicalComplexity: 2, RequirementUncertainty: 1, ChangeScope: 2,
			ValidationBurden: 2, HumanDependency: 1, RiskAndReversibility: 1,
		},
		Confidence: 0.8, Rationale: "涉及查询参数、持久化和前端筛选状态",
		Assumptions: []string{"现有搜索接口可以扩展"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if stored.Revision != 1 || stored.Complexity != assessment.ComplexityC2 || stored.Autonomy != assessment.AutonomyA2 {
		t.Fatalf("assessment = %#v", stored)
	}
	if updated.Title != "实现搜索组合筛选" || updated.TitleSource != task.TitleSourceAI || updated.TitleLocked {
		t.Fatalf("updated task = %#v", updated)
	}
	if updated.Status != task.StatusReady || updated.AssessmentInputVersion != created.AssessmentInputVersion {
		t.Fatalf("triage changed execution state = %#v", updated)
	}
	assessmentStore := NewAssessmentStore(database)
	current, err := assessmentStore.GetCurrent(ctx, created.ID)
	if err != nil || current.ID != stored.ID {
		t.Fatalf("GetCurrent() = %#v, %v", current, err)
	}
	history, err := assessmentStore.List(ctx, created.ID)
	if err != nil || len(history) != 1 || history[0].ID != stored.ID {
		t.Fatalf("List() = %#v, %v", history, err)
	}
	allCurrent, err := assessmentStore.ListCurrent(ctx)
	if err != nil || allCurrent[created.ID].ID != stored.ID {
		t.Fatalf("ListCurrent() = %#v, %v", allCurrent, err)
	}
	if _, err := assessmentStore.GetCurrent(ctx, "missing"); err != ErrAssessmentNotFound {
		t.Fatalf("missing GetCurrent() error = %v", err)
	}
}

func TestAssessmentStoreNeverOverwritesHumanTitle(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureProfile(t, database, agentprofile.RoleTriager)
	created, err := NewTaskStore(database).Create(ctx, task.CreateInput{
		Title: "人工锁定标题", Description: "评估这个任务",
	})
	if err != nil {
		t.Fatal(err)
	}
	claim := startTriageRun(t, database)
	stored, updated, err := NewAssessmentStore(database).ApplyTriageResult(ctx, claim.Run.ID, assessment.Input{
		SuggestedTitle: "AI 不应覆盖标题",
		Scores:         assessment.DimensionScores{HumanDependency: 4},
		Confidence:     0.7, Rationale: "需要人工业务判断",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != created.Title || updated.TitleSource != task.TitleSourceHuman || !updated.TitleLocked {
		t.Fatalf("updated task = %#v", updated)
	}
	if stored.AppliedTitle != created.Title {
		t.Fatalf("applied title = %q", stored.AppliedTitle)
	}
}

func TestFailedTriageFallsBackToImplementationInsteadOfLooping(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureProfile(t, database, agentprofile.RoleTriager)
	configureProfile(t, database, agentprofile.RoleImplementer)
	created, err := NewTaskStore(database).Create(ctx, task.CreateInput{Description: "先评估再实现"})
	if err != nil {
		t.Fatal(err)
	}
	runs := NewRunStore(database)
	claim := startTriageRun(t, database)
	retryable := false
	if _, err := runs.Finish(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, RunFinish{
		Status: run.StatusFailed, ExitCode: 1, FailureKind: "RESULT",
		FailureCode: "INVALID_TRIAGE_RESULT", FailureRetryable: &retryable,
	}); err != nil {
		t.Fatal(err)
	}
	next, err := runs.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if next.Run.Purpose != run.PurposeImplementation || next.Run.TaskID != created.ID {
		t.Fatalf("next claim = %#v", next)
	}
}

func startTriageRun(t *testing.T, database *sql.DB) run.Claim {
	t.Helper()
	ctx := context.Background()
	store := NewRunStore(database)
	claim, err := store.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Start(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 123, strings.Repeat("a", 64), time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := store.MarkRunning(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	return claim
}
