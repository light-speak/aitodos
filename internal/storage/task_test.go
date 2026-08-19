package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
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
		Priority:           10,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Status != task.StatusBacklog || created.Version != 1 {
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

	queued, err := store.ApplyCommand(ctx, created.ID, created.Version, task.CommandQueue)
	if err != nil {
		t.Fatalf("ApplyCommand() error = %v", err)
	}
	if queued.Status != task.StatusReady || queued.Version != 2 {
		t.Fatalf("queued task = %#v", queued)
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
	if loaded.Status != task.StatusBacklog || loaded.Version != 1 {
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
	if _, err := store.ApplyCommand(ctx, created.ID, created.Version, task.CommandQueue); err != nil {
		t.Fatal(err)
	}

	_, err = store.ApplyCommand(ctx, created.ID, created.Version, task.CommandQueue)
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

func TestTaskStoreReturnsNotFound(t *testing.T) {
	store := NewTaskStore(openTaskTestDatabase(t))
	_, err := store.Get(context.Background(), "missing")
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("Get() error = %v, want ErrTaskNotFound", err)
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
