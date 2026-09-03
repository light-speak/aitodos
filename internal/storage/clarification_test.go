package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/domain/agentprofile"
	"github.com/light-speak/aitodos/internal/domain/clarification"
	domainrun "github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/topic"
)

func TestTriageClarificationKeepsTaskReadyAndResumesTriage(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureProfile(t, database, agentprofile.RoleTriager)
	tasks := NewTaskStore(database)
	created, err := tasks.Create(ctx, task.CreateInput{Description: "做一个导出功能"})
	if err != nil {
		t.Fatal(err)
	}
	runs := NewRunStore(database)
	claim := startTriageRun(t, database)
	question := clarification.Request{
		Category: clarification.CategoryRequirement,
		Question: "需要导出什么格式？",
		Options: []clarification.Option{
			{ID: "json", Label: "JSON", Description: "机器可读"},
			{ID: "csv", Label: "CSV", Description: "表格可读"},
		},
		RecommendedOptionID: "json",
	}
	finished, createdQuestion, err := runs.FinishNeedsInput(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, question)
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != domainrun.StatusNeedsInput || createdQuestion.ContinuationPurpose != domainrun.PurposeTriage {
		t.Fatalf("finished = %#v, question = %#v", finished, createdQuestion)
	}
	unchanged, err := tasks.Get(ctx, created.ID)
	if err != nil || unchanged.Status != task.StatusReady || unchanged.Version != created.Version {
		t.Fatalf("task = %#v, %v", unchanged, err)
	}
	answered, resumed, err := NewClarificationStore(database).Answer(ctx, createdQuestion.ID, clarification.AnswerInput{
		SelectedOptionID: "json", ExpectedVersion: createdQuestion.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if answered.Status != clarification.StatusAnswered || resumed.Version != created.Version {
		t.Fatalf("answered = %#v, resumed = %#v", answered, resumed)
	}
	continuation, err := runs.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if continuation.Run.Purpose != domainrun.PurposeTriage || continuation.Run.ContinuationOfRunID != claim.Run.ID {
		t.Fatalf("continuation = %#v", continuation)
	}
}

func TestPlanningClarificationResumesSameTopic(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureProfile(t, database, agentprofile.RolePlanner)
	created, err := NewTopicStore(database).Create(ctx, topic.CreateInput{Title: "选择发布策略"})
	if err != nil {
		t.Fatal(err)
	}
	runs := NewRunStore(database)
	claim, err := runs.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runs.Start(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 123, strings.Repeat("a", 64), time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := runs.MarkRunning(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	_, question, err := runs.FinishNeedsInput(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, clarification.Request{
		Category: clarification.CategoryDecision, Question: "公共仓库是否只走 PR？",
		Options:             []clarification.Option{{ID: "pr", Label: "只走 PR"}, {ID: "direct", Label: "允许直推"}},
		RecommendedOptionID: "pr",
	})
	if err != nil {
		t.Fatal(err)
	}
	if question.TopicID != created.ID || question.TaskID != "" || question.ContinuationPurpose != domainrun.PurposePlanning {
		t.Fatalf("planning question = %#v", question)
	}
	topicQuestions, err := NewClarificationStore(database).ListTopic(ctx, created.ID)
	if err != nil || len(topicQuestions) != 1 || topicQuestions[0].ID != question.ID {
		t.Fatalf("ListTopic() = %#v, %v", topicQuestions, err)
	}
	answer, err := NewClarificationStore(database).AnswerSubject(ctx, question.ID, clarification.AnswerInput{
		SelectedOptionID: "pr", ExpectedVersion: question.Version,
	})
	if err != nil {
		t.Fatal(err)
	}
	if answer.Topic == nil || answer.Topic.Version != created.Version+1 || answer.Task != nil {
		t.Fatalf("answer = %#v", answer)
	}
	continuation, err := runs.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if continuation.Run.TopicID != created.ID || continuation.Run.ContinuationOfRunID != claim.Run.ID {
		t.Fatalf("continuation = %#v", continuation)
	}
	linked, err := NewClarificationStore(database).GetForContinuationRun(ctx, continuation.Run.ID)
	if err != nil || linked.ID != question.ID {
		t.Fatalf("linked clarification = %#v, %v", linked, err)
	}
}

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
	if _, err := runs.Start(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 123, strings.Repeat("a", 64), time.Hour); err != nil {
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
	taskQuestions, err := NewClarificationStore(database).ListTask(ctx, created.ID)
	if err != nil || len(taskQuestions) != 1 || taskQuestions[0].ID != question.ID {
		t.Fatalf("ListTask() = %#v, %v", taskQuestions, err)
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
