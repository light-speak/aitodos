package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/domain/clarification"
	domainrun "github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/task"
)

func TestClarificationFinishesRunAndAnswerCreatesContinuation(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureRunnableProfiles(t, database)
	tasks := NewTaskStore(database)
	created, err := tasks.Create(ctx, task.CreateInput{Title: "实现可恢复问答", Priority: 1})
	if err != nil {
		t.Fatal(err)
	}
	runs := NewRunStore(database)
	claim, err := runs.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runs.Start(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 123, time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := runs.MarkRunning(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	finished, question, err := runs.FinishNeedsInput(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, clarification.Request{
		Category: clarification.CategoryDecision,
		Question: "数据库迁移是否兼容旧版本？",
		Options: []clarification.Option{
			{ID: "compatible", Label: "兼容升级", Description: "保留旧数据"},
			{ID: "fresh", Label: "仅新项目", Description: "不迁移旧数据"},
		},
		RecommendedOptionID: "compatible",
	})
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != domainrun.StatusNeedsInput || question.Status != clarification.StatusOpen {
		t.Fatalf("finish = %#v, clarification = %#v", finished, question)
	}
	blocked, err := tasks.Get(ctx, created.ID)
	if err != nil || blocked.Status != task.StatusBlocked {
		t.Fatalf("blocked task = %#v, %v", blocked, err)
	}

	answered, resumedTask, err := NewClarificationStore(database).Answer(ctx, question.ID, clarification.AnswerInput{
		SelectedOptionID: "compatible", ExpectedVersion: question.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if answered.Status != clarification.StatusAnswered || resumedTask.Status != task.StatusReady {
		t.Fatalf("answered = %#v, task = %#v", answered, resumedTask)
	}
	if _, _, err := NewClarificationStore(database).Answer(ctx, question.ID, clarification.AnswerInput{
		SelectedOptionID: "fresh", ExpectedVersion: question.Version,
	}); !errors.Is(err, ErrClarificationConflict) {
		t.Fatalf("second Answer() error = %v", err)
	}

	continuation, err := runs.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if continuation.Run.ContinuationOfRunID != claim.Run.ID || continuation.Run.Purpose != domainrun.PurposeImplementation {
		t.Fatalf("continuation claim = %#v", continuation)
	}
	linked, err := NewClarificationStore(database).GetForContinuationRun(ctx, continuation.Run.ID)
	if err != nil || linked.ID != question.ID {
		t.Fatalf("continuation clarification = %#v, %v", linked, err)
	}
}
