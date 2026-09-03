package storage

import (
	"context"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/domain/agentprofile"
	"github.com/light-speak/aitodos/internal/domain/discussion"
	"github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/taskfeedback"
)

func TestTaskFeedbackDiscussionClaimsReadOnlyReviewWithoutChangingTaskState(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureProfile(t, database, agentprofile.RoleReviewer)
	created, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "检查实现"})
	if err != nil {
		t.Fatal(err)
	}
	message, feedback, err := NewTaskFeedbackStore(database).Discuss(ctx, created.ID, discussion.CreateMessageInput{Content: "这个实现还有什么缺陷？"})
	if err != nil || feedback.Status != taskfeedback.StatusQueued || feedback.SourceMessageID != message.ID {
		t.Fatalf("Discuss() = %#v, %#v, %v", message, feedback, err)
	}
	feedbackStore := NewTaskFeedbackStore(database)
	pending, err := feedbackStore.HasPendingTask(ctx, created.ID)
	if err != nil || !pending {
		t.Fatalf("HasPendingTask() = %v, %v", pending, err)
	}

	runs := NewRunStore(database)
	claim, err := runs.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if claim.Run.Purpose != run.PurposeReview || claim.Run.TaskID != created.ID {
		t.Fatalf("review claim = %#v", claim.Run)
	}
	question, err := feedbackStore.QuestionForRun(ctx, claim.Run.ID)
	if err != nil || question.ID != message.ID {
		t.Fatalf("QuestionForRun() = %#v, %v", question, err)
	}
	loaded, err := NewTaskStore(database).Get(ctx, created.ID)
	if err != nil || loaded.Status != task.StatusReady {
		t.Fatalf("task after review claim = %#v, %v", loaded, err)
	}
	if _, err := runs.Start(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, 123, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, err := runs.MarkRunning(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	if _, err := runs.BeginFinalization(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration, FinalizationIntent{
		Finish: RunFinish{Status: run.StatusSucceeded, ExitCode: 0}, TaskReply: "当前缺少边界测试。",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := runs.CompleteFinalization(ctx, claim.Run.ID, claim.ClaimToken, claim.Run.LeaseGeneration); err != nil {
		t.Fatal(err)
	}
	messages, err := NewDiscussionStore(database).ListTaskMessages(ctx, created.ID)
	if err != nil || len(messages) != 2 || messages[1].AuthorKind != discussion.AuthorAgent || messages[1].Content != "当前缺少边界测试。" {
		t.Fatalf("task discussion = %#v, %v", messages, err)
	}
	finished, err := NewTaskFeedbackStore(database).Get(ctx, feedback.ID)
	if err != nil || finished.Status != taskfeedback.StatusAnswered || finished.RunID != claim.Run.ID || finished.ResponseMessageID != messages[1].ID {
		t.Fatalf("finished feedback = %#v, %v", finished, err)
	}
	pending, err = feedbackStore.HasPendingTask(ctx, created.ID)
	if err != nil || pending {
		t.Fatalf("HasPendingTask() after answer = %v, %v", pending, err)
	}
	if _, err := feedbackStore.QuestionForRun(ctx, "missing"); err != ErrTaskFeedbackNotFound {
		t.Fatalf("missing QuestionForRun() error = %v", err)
	}
}

func TestTaskFeedbackRequestChangesRejectsReviewAtomically(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	tasks := NewTaskStore(database)
	created, err := tasks.Create(ctx, task.CreateInput{Title: "修复权限检查"})
	if err != nil {
		t.Fatal(err)
	}
	inReview, err := tasks.ApplyCommand(ctx, created.ID, created.Version, task.CommandSubmitReview)
	if err != nil {
		t.Fatal(err)
	}
	message, feedback, updated, followUp, err := NewTaskFeedbackStore(database).RequestChanges(
		ctx, inReview.ID, inReview.Version, discussion.CreateMessageInput{Content: "未登录时仍然可以访问。"},
	)
	if err != nil || followUp != nil || updated.Status != task.StatusChangesRequested || feedback.Status != taskfeedback.StatusApplied {
		t.Fatalf("RequestChanges() = %#v, %#v, %#v, %#v, %v", message, feedback, updated, followUp, err)
	}
	reviews, err := tasks.ListReviews(ctx, created.ID)
	if err != nil || len(reviews) != 1 || reviews[0].Decision != task.ReviewRejected || reviews[0].Comment != message.Content {
		t.Fatalf("reviews = %#v, %v", reviews, err)
	}
}

func TestTaskFeedbackRequestChangesCreatesFollowUpForAcceptedTask(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	tasks := NewTaskStore(database)
	created, err := tasks.Create(ctx, task.CreateInput{Title: "已经发布的登录功能", TargetBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	inReview, err := tasks.ApplyCommand(ctx, created.ID, created.Version, task.CommandSubmitReview)
	if err != nil {
		t.Fatal(err)
	}
	accepted, _, err := tasks.ApplyReview(ctx, inReview.ID, inReview.Version, task.ReviewInput{Decision: task.ReviewAccepted}, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	_, feedback, unchanged, followUp, err := NewTaskFeedbackStore(database).RequestChanges(
		ctx, accepted.ID, accepted.Version, discussion.CreateMessageInput{Content: "退出后令牌仍然有效，需要修复。"},
	)
	if err != nil || unchanged.Status != task.StatusAccepted || followUp == nil || feedback.Status != taskfeedback.StatusApplied {
		t.Fatalf("accepted feedback = %#v, %#v, %#v, %v", unchanged, followUp, feedback, err)
	}
	if followUp.Status != task.StatusReady || followUp.TargetBranch != accepted.TargetBranch {
		t.Fatalf("follow-up task = %#v", followUp)
	}
	links, err := NewRelationStore(database).ListTaskRelations(ctx, accepted.ID)
	if err != nil || len(links) != 1 || links[0].Task.ID != followUp.ID {
		t.Fatalf("follow-up links = %#v, %v", links, err)
	}
}

func TestTaskFeedbackFailureCanRetryWithoutOverwritingRunHistory(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	configureProfile(t, database, agentprofile.RoleReviewer)
	created, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "检查并发错误"})
	if err != nil {
		t.Fatal(err)
	}
	feedbackStore := NewTaskFeedbackStore(database)
	_, original, err := feedbackStore.Discuss(ctx, created.ID, discussion.CreateMessageInput{Content: "并发关闭时会不会丢数据？"})
	if err != nil {
		t.Fatal(err)
	}
	runs := NewRunStore(database)
	first, err := runs.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runs.Finish(ctx, first.Run.ID, first.ClaimToken, first.Run.LeaseGeneration, RunFinish{
		Status: run.StatusFailed, ExitCode: 1, FailureMessage: "Agent 进程退出",
	}); err != nil {
		t.Fatal(err)
	}

	retry, err := feedbackStore.Retry(ctx, original.ID)
	if err != nil {
		t.Fatal(err)
	}
	if retry.RetryOfFeedbackID != original.ID || retry.SourceMessageID != original.SourceMessageID || retry.Status != taskfeedback.StatusQueued {
		t.Fatalf("retry feedback = %#v", retry)
	}
	second, err := runs.ClaimNextTask(ctx, 1, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second.Run.RetryOfRunID != first.Run.ID {
		t.Fatalf("retry run = %#v", second.Run)
	}
	loadedOriginal, err := feedbackStore.Get(ctx, original.ID)
	if err != nil || loadedOriginal.Status != taskfeedback.StatusFailed || loadedOriginal.RunID != first.Run.ID {
		t.Fatalf("original feedback = %#v, %v", loadedOriginal, err)
	}
	items, err := feedbackStore.ListTask(ctx, created.ID)
	if err != nil || len(items) != 2 {
		t.Fatalf("feedback list = %#v, %v", items, err)
	}
	events, err := feedbackStore.ListEvents(ctx, created.ID, 0, 20)
	if err != nil || len(events) != 5 || events[0].Sequence != 1 || events[4].Status != taskfeedback.StatusRunning {
		t.Fatalf("feedback events = %#v, %v", events, err)
	}
}
