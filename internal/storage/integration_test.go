package storage

import (
	"context"
	"database/sql"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/integration"
	"github.com/light-speak/aitodos/internal/domain/task"
)

func TestIntegrationStorePersistsCompletionAndTargetSync(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	firstTask, firstReview := createAcceptedStorageTask(t, database, "集成任务一")
	store := NewIntegrationStore(database)
	first, err := store.Reserve(ctx, integration.ReserveInput{
		TaskID: firstTask.ID, ReviewID: firstReview.ID, Operation: integration.OperationIntegrate,
		TargetBranch: "main", SourceCommitSHA: "abcdef123", TargetBeforeSHA: "123456789",
	})
	if err != nil || first.Status != integration.StatusRunning {
		t.Fatalf("first = %#v, %v", first, err)
	}
	running, err := store.ListRunning(ctx)
	if err != nil || len(running) != 1 || running[0].ID != first.ID {
		t.Fatalf("running = %#v, %v", running, err)
	}
	completed, err := store.Complete(ctx, first.ID, integration.StatusSucceeded, "abcdef123", "abcdef123", "", "")
	if err != nil || completed.Status != integration.StatusSucceeded || completed.TargetAfterSHA != "abcdef123" {
		t.Fatalf("completed = %#v, %v", completed, err)
	}
	latest, err := store.LatestForTask(ctx, firstTask.ID)
	if err != nil || latest == nil || latest.ID != first.ID {
		t.Fatalf("latest = %#v, %v", latest, err)
	}

	secondTask, secondReview := createAcceptedStorageTask(t, database, "集成任务二")
	second, err := store.Reserve(ctx, integration.ReserveInput{
		TaskID: secondTask.ID, ReviewID: secondReview.ID, Operation: integration.OperationSync,
		TargetBranch: "main", SourceCommitSHA: "fedcba987", TargetBeforeSHA: "987654321",
	})
	if err != nil {
		t.Fatal(err)
	}
	updatedTask, synced, err := store.CompleteSync(ctx, second.ID, integration.StatusSynced, "fedcba999", "", "")
	if err != nil || updatedTask.Status != task.StatusChangesRequested || synced.Status != integration.StatusSynced {
		t.Fatalf("sync = %#v, %#v, %v", updatedTask, synced, err)
	}
	empty, err := store.LatestForTask(ctx, "missing")
	if err != nil || empty != nil {
		t.Fatalf("missing latest = %#v, %v", empty, err)
	}
}

func createAcceptedStorageTask(t *testing.T, database *sql.DB, title string) (task.Task, task.Review) {
	t.Helper()
	store := NewTaskStore(database)
	created, err := store.Create(context.Background(), task.CreateInput{Title: title})
	if err != nil {
		t.Fatal(err)
	}
	reviewing, err := store.ApplyCommand(context.Background(), created.ID, created.Version, task.CommandSubmitReview)
	if err != nil {
		t.Fatal(err)
	}
	accepted, review, err := store.ApplyReview(context.Background(), reviewing.ID, reviewing.Version, task.ReviewInput{Decision: task.ReviewAccepted}, "abcdef123")
	if err != nil {
		t.Fatal(err)
	}
	return accepted, review
}
