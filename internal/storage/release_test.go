package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/release"
	"github.com/light-speak/aitodos/internal/domain/task"
)

func TestReleaseStoreLifecycleIsIdempotentAndImmutable(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	createdTask, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "发布任务"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewReleaseStore(database)
	input := release.CreateInput{Version: "1.2.3", SourceBranch: "main", TaskIDs: []string{createdTask.ID}}
	created, err := store.Reserve(ctx, input, "abcdef123456")
	if err != nil || created.Status != release.StatusCreating || len(created.TaskIDs) != 1 {
		t.Fatalf("created = %#v, %v", created, err)
	}
	again, err := store.Reserve(ctx, input, "abcdef123456")
	if err != nil || again.ID != created.ID {
		t.Fatalf("idempotent reserve = %#v, %v", again, err)
	}
	if _, err := store.Reserve(ctx, input, "fedcba654321"); !errors.Is(err, ErrReleaseConflict) {
		t.Fatalf("changed commit error = %v", err)
	}
	if err := store.MarkFailed(ctx, created.ID, "tag 冲突"); err != nil {
		t.Fatal(err)
	}
	tagged, err := store.MarkTagged(ctx, created.ID)
	if err != nil || tagged.Status != release.StatusTagged || tagged.TaggedAt == nil || tagged.FailureMessage != "" {
		t.Fatalf("tagged = %#v, %v", tagged, err)
	}
	items, err := store.List(ctx)
	if err != nil || len(items) != 1 || items[0].ID != created.ID || len(items[0].TaskIDs) != 1 {
		t.Fatalf("releases = %#v, %v", items, err)
	}
	if _, err := store.Reserve(ctx, release.CreateInput{Version: "2.0.0", SourceBranch: "main", TaskIDs: []string{"missing"}}, "abcdef123456"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("missing task error = %v", err)
	}
}
