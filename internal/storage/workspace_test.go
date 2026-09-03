package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/workspace"
)

func TestWorkspaceStoreLifecycleAndTaskLink(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	createdTask, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "Workspace 生命周期"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewWorkspaceStore(database)
	reserved, err := store.Reserve(ctx, createdTask.ID, "/repo/.ats/worktrees/task-1", "ats/task-1", "main", "abcdef1")
	if err != nil || reserved.State != workspace.StateProvisioning {
		t.Fatalf("reserved = %#v, %v", reserved, err)
	}
	again, err := store.Reserve(ctx, createdTask.ID, "/other", "other", "dev", "7654321")
	if err != nil || again.ID != reserved.ID || again.Path != reserved.Path {
		t.Fatalf("idempotent reserve = %#v, %v", again, err)
	}
	ready, err := store.MarkReady(ctx, reserved.ID, "abcdef2", false)
	if err != nil || ready.State != workspace.StateReady || ready.HeadSHA != "abcdef2" || ready.LastVerifiedAt == nil {
		t.Fatalf("ready = %#v, %v", ready, err)
	}
	linkedTask, err := NewTaskStore(database).Get(ctx, createdTask.ID)
	if err != nil || linkedTask.CurrentWorkspaceID != reserved.ID || linkedTask.TargetBranch != "main" {
		t.Fatalf("linked task = %#v, %v", linkedTask, err)
	}
	dirty, err := store.MarkReady(ctx, reserved.ID, "abcdef3", true)
	if err != nil || dirty.State != workspace.StateDirty || !dirty.Dirty {
		t.Fatalf("dirty = %#v, %v", dirty, err)
	}
	if err := store.MarkError(ctx, reserved.ID, "创建失败"); err != nil {
		t.Fatal(err)
	}
	if err := store.MarkQuarantined(ctx, reserved.ID, "身份不匹配"); err != nil {
		t.Fatal(err)
	}
	quarantined, err := store.GetByTask(ctx, createdTask.ID)
	if err != nil || quarantined.State != workspace.StateQuarantined || quarantined.FailureMessage != "身份不匹配" {
		t.Fatalf("quarantined = %#v, %v", quarantined, err)
	}
	if _, err := store.GetByTask(ctx, "missing"); !errors.Is(err, ErrWorkspaceNotFound) {
		t.Fatalf("missing workspace error = %v", err)
	}
}
