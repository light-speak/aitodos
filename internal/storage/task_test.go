package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/task"
)

func TestTaskStoreCreatesListsAndTransitionsTaskAtomically(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	store := NewTaskStore(database)

	created, err := store.Create(ctx, task.CreateInput{
		Title:              "实现任务看板",
		Description:        "显示项目任务",
		AcceptanceCriteria: "可以创建并排队任务",
		Priority:           0,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Status != task.StatusReady || created.Version != 1 || created.Priority != 0 {
		t.Fatalf("created task = %#v", created)
	}
	if created.ID == "" || created.Key == "" {
		t.Fatalf("created identity = %#v", created)
	}

	listed, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("List() = %#v", listed)
	}

	running, err := store.ApplyCommand(ctx, created.ID, created.Version, task.CommandClaimRun)
	if err != nil {
		t.Fatalf("ApplyCommand() error = %v", err)
	}
	if running.Status != task.StatusRunning || running.Version != 2 {
		t.Fatalf("running task = %#v", running)
	}

	events, err := store.ListEvents(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events count = %d, want 2", len(events))
	}
	if events[0].Sequence != 1 || events[0].Type != task.EventCreated {
		t.Fatalf("created event = %#v", events[0])
	}
	if events[1].Sequence != 2 || events[1].Type != task.EventStatusChanged {
		t.Fatalf("transition event = %#v", events[1])
	}
}

func TestTaskStoreRejectsInvalidTransitionWithoutPartialWrite(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	store := NewTaskStore(database)
	created, err := store.Create(ctx, task.CreateInput{Title: "任务"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.ApplyCommand(ctx, created.ID, created.Version, task.CommandAccept)
	var transitionErr *task.TransitionError
	if !errors.As(err, &transitionErr) {
		t.Fatalf("ApplyCommand() error = %v, want *task.TransitionError", err)
	}

	loaded, err := store.Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != task.StatusReady || loaded.Version != 1 {
		t.Fatalf("task changed after rejection: %#v", loaded)
	}
	events, err := store.ListEvents(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events count = %d, want 1", len(events))
	}
}

func TestTaskStoreRejectsStaleVersionWithoutPartialWrite(t *testing.T) {
	ctx := context.Background()
	store := NewTaskStore(openTaskTestDatabase(t))
	created, err := store.Create(ctx, task.CreateInput{Title: "任务"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyCommand(ctx, created.ID, created.Version, task.CommandClaimRun); err != nil {
		t.Fatal(err)
	}

	_, err = store.ApplyCommand(ctx, created.ID, created.Version, task.CommandClaimRun)
	if !errors.Is(err, ErrTaskVersionConflict) {
		t.Fatalf("ApplyCommand() error = %v, want ErrTaskVersionConflict", err)
	}
	events, err := store.ListEvents(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("events count = %d, want 2", len(events))
	}
}

func TestTaskStoreUpdatesTargetBranchBeforeWorkspaceOnly(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	store := NewTaskStore(database)
	created, err := store.Create(ctx, task.CreateInput{Title: "选择发布分支", TargetBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}

	updated, err := store.UpdateTargetBranch(ctx, created.ID, created.Version, "release/macos")
	if err != nil {
		t.Fatal(err)
	}
	if updated.TargetBranch != "release/macos" || updated.Version != 2 || updated.AssessmentInputVersion != 2 {
		t.Fatalf("updated task = %#v", updated)
	}
	events, err := store.ListEvents(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].Type != task.EventTargetBranchChanged {
		t.Fatalf("events = %#v", events)
	}

	_, err = database.ExecContext(ctx, `UPDATE tasks SET current_workspace_id = 'workspace-1' WHERE id = ?`, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.UpdateTargetBranch(ctx, created.ID, updated.Version, "main")
	if !errors.Is(err, ErrTaskWorkspaceExists) {
		t.Fatalf("UpdateTargetBranch() error = %v, want ErrTaskWorkspaceExists", err)
	}
}

func TestTaskStoreReturnsNotFound(t *testing.T) {
	store := NewTaskStore(openTaskTestDatabase(t))
	_, err := store.Get(context.Background(), "missing")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("Get() error = %v, want ErrTaskNotFound", err)
	}
}

func TestTaskStoreUpdatesExecutionInputCancelsAndArchives(t *testing.T) {
	ctx := context.Background()
	store := NewTaskStore(openTaskTestDatabase(t))
	created, err := store.Create(ctx, task.CreateInput{Title: "完善导出", Description: "旧范围", Priority: 2})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdateDetails(ctx, created.ID, created.Version, task.UpdateDetailsInput{
		Description: "导出当前项目", AcceptanceCriteria: "生成可恢复 ZIP", Priority: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Priority != 0 || updated.AcceptanceCriteria != "生成可恢复 ZIP" || updated.AssessmentInputVersion != 2 {
		t.Fatalf("updated = %#v", updated)
	}
	cancelled, err := store.ApplyCommand(ctx, updated.ID, updated.Version, task.CommandCancelTask)
	if err != nil {
		t.Fatal(err)
	}
	archived, err := store.Archive(ctx, cancelled.ID, cancelled.Version)
	if err != nil {
		t.Fatal(err)
	}
	if archived.ArchivedAt == nil {
		t.Fatalf("archived = %#v", archived)
	}
	listed, err := store.List(ctx)
	if err != nil || len(listed) != 0 {
		t.Fatalf("List() = %#v, %v", listed, err)
	}
	all, err := store.ListIncludingArchived(ctx)
	if err != nil || len(all) != 1 {
		t.Fatalf("ListIncludingArchived() = %#v, %v", all, err)
	}
	events, err := store.ListEvents(ctx, created.ID)
	if err != nil || events[len(events)-1].Type != task.EventArchived {
		t.Fatalf("events = %#v, %v", events, err)
	}
}

func TestTaskStoreRecordsReviewWithStatusTransitionAtomically(t *testing.T) {
	ctx := context.Background()
	store := NewTaskStore(openTaskTestDatabase(t))
	created, err := store.Create(ctx, task.CreateInput{Title: "检查 Diff"})
	if err != nil {
		t.Fatal(err)
	}
	inReview, err := store.ApplyCommand(ctx, created.ID, created.Version, task.CommandSubmitReview)
	if err != nil {
		t.Fatal(err)
	}
	accepted, review, err := store.ApplyReview(ctx, inReview.ID, inReview.Version, task.ReviewInput{
		Decision: task.ReviewAccepted,
		Comment:  "符合验收标准",
	}, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Status != task.StatusAccepted || review.CommitSHA != "abc123" {
		t.Fatalf("accepted = %#v, review = %#v", accepted, review)
	}
	reviews, err := store.ListReviews(ctx, created.ID)
	if err != nil || len(reviews) != 1 || reviews[0].Comment != "符合验收标准" {
		t.Fatalf("ListReviews() = %#v, %v", reviews, err)
	}
}

func TestTaskStoreCoversEditArchiveAndReviewConflicts(t *testing.T) {
	ctx := context.Background()
	store := NewTaskStore(openTaskTestDatabase(t))
	created, err := store.Create(ctx, task.CreateInput{Title: "编辑冲突", TargetBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := store.UpdateTitle(ctx, created.ID, created.Version, task.UpdateTitleInput{Title: "人工标题"})
	if err != nil || updated.Title != "人工标题" || !updated.TitleLocked {
		t.Fatalf("updated title = %#v, %v", updated, err)
	}
	if _, err := store.UpdateTitle(ctx, updated.ID, created.Version, task.UpdateTitleInput{Title: "过期标题"}); !errors.Is(err, ErrTaskVersionConflict) {
		t.Fatalf("stale title error = %v", err)
	}
	unchanged, err := store.UpdateTargetBranch(ctx, updated.ID, updated.Version, "main")
	if err != nil || unchanged.Version != updated.Version {
		t.Fatalf("unchanged branch = %#v, %v", unchanged, err)
	}
	if _, err := store.UpdateDetails(ctx, updated.ID, created.Version, task.UpdateDetailsInput{Description: "过期"}); !errors.Is(err, ErrTaskVersionConflict) {
		t.Fatalf("stale details error = %v", err)
	}
	running, err := store.ApplyCommand(ctx, updated.ID, updated.Version, task.CommandClaimRun)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateDetails(ctx, running.ID, running.Version, task.UpdateDetailsInput{Description: "执行中修改"}); !errors.Is(err, ErrTaskEditState) {
		t.Fatalf("running edit error = %v", err)
	}
	if _, err := store.Archive(ctx, running.ID, running.Version); !errors.Is(err, ErrTaskArchiveState) {
		t.Fatalf("running archive error = %v", err)
	}
	if _, _, err := store.ApplyReview(ctx, running.ID, running.Version, task.ReviewInput{Decision: task.ReviewAccepted, Comment: "不在验收态"}, "sha"); err == nil {
		t.Fatal("ApplyReview() accepted a running task")
	}
	blocked, err := store.ApplyCommand(ctx, running.ID, running.Version, task.CommandCancelRun)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, err := store.ApplyCommand(ctx, blocked.ID, blocked.Version, task.CommandCancelTask)
	if err != nil {
		t.Fatal(err)
	}
	archived, err := store.Archive(ctx, cancelled.ID, cancelled.Version)
	if err != nil {
		t.Fatal(err)
	}
	again, err := store.Archive(ctx, archived.ID, archived.Version)
	if err != nil || again.Version != archived.Version {
		t.Fatalf("idempotent archive = %#v, %v", again, err)
	}
}

func TestTaskStoreReturnsErrorsAfterDatabaseClose(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	store := NewTaskStore(database)
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	calls := []func() error{
		func() error { _, err := store.Create(ctx, task.CreateInput{Title: "任务"}); return err },
		func() error { _, err := store.Get(ctx, "task"); return err },
		func() error {
			_, err := store.UpdateTitle(ctx, "task", 1, task.UpdateTitleInput{Title: "标题"})
			return err
		},
		func() error {
			_, err := store.UpdateDetails(ctx, "task", 1, task.UpdateDetailsInput{Description: "描述", AcceptanceCriteria: "验收", Priority: 1})
			return err
		},
		func() error { _, err := store.Archive(ctx, "task", 1); return err },
		func() error { _, err := store.UpdateTargetBranch(ctx, "task", 1, "main"); return err },
		func() error { _, err := store.List(ctx); return err },
		func() error { _, err := store.ListIncludingArchived(ctx); return err },
		func() error { _, err := store.ApplyCommand(ctx, "task", 1, task.CommandCancelTask); return err },
		func() error { _, err := store.RetryBlocked(ctx, "task", 1); return err },
		func() error { _, err := store.ListEvents(ctx, "task"); return err },
		func() error {
			_, _, err := store.ApplyReview(ctx, "task", 1, task.ReviewInput{Decision: task.ReviewRejected, Comment: "需修改"}, "sha")
			return err
		},
		func() error { _, err := store.ListReviews(ctx, "task"); return err },
	}
	for index, call := range calls {
		if err := call(); err == nil || strings.TrimSpace(err.Error()) == "" {
			t.Fatalf("closed database call %d error = %v", index, err)
		}
	}
}

func openTaskTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := Open(context.Background(), filepath.Join(t.TempDir(), "state.db"), ProjectMetadata{
		InstanceID:   "task-test-instance",
		Name:         "task-test",
		RepoRoot:     "/tmp/task-test",
		GitCommonDir: "/tmp/task-test/.git",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}
