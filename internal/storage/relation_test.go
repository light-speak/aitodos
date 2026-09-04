package storage

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/relation"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/topic"
)

func TestRelationStoreLinksTopicsAndTasksIdempotently(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	store := NewRelationStore(database)
	createdTopic, err := NewTopicStore(database).Create(ctx, topic.CreateInput{Title: "搜索方案"})
	if err != nil {
		t.Fatal(err)
	}
	createdTask, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "实现索引"})
	if err != nil {
		t.Fatal(err)
	}

	for attempt := 0; attempt < 2; attempt++ {
		if err := store.LinkTopicTask(ctx, createdTopic.ID, createdTask.ID); err != nil {
			t.Fatalf("LinkTopicTask() error = %v", err)
		}
	}
	linked, err := store.ListTopicTasks(ctx, createdTopic.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(linked) != 1 || linked[0].Task.ID != createdTask.ID {
		t.Fatalf("linked tasks = %#v", linked)
	}
	linkedTopics, err := store.ListTaskTopics(ctx, createdTask.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(linkedTopics) != 1 || linkedTopics[0].Topic.ID != createdTopic.ID {
		t.Fatalf("linked topics = %#v", linkedTopics)
	}
	if err := store.UnlinkTopicTask(ctx, createdTopic.ID, createdTask.ID); err != nil {
		t.Fatal(err)
	}
	linked, err = store.ListTopicTasks(ctx, createdTopic.ID)
	if err != nil || len(linked) != 0 {
		t.Fatalf("linked tasks after unlink = %#v, %v", linked, err)
	}
}

func TestRelationStorePreservesTypedDirection(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	tasks := NewTaskStore(database)
	blocker, err := tasks.Create(ctx, task.CreateInput{Title: "先完成数据库迁移"})
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := tasks.Create(ctx, task.CreateInput{Title: "再上线接口"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewRelationStore(database)
	if err := store.LinkTasksTyped(ctx, blocker.ID, blocked.ID, relation.TypeBlocks); err != nil {
		t.Fatal(err)
	}
	outgoing, err := store.ListTaskRelations(ctx, blocker.ID)
	if err != nil {
		t.Fatal(err)
	}
	incoming, err := store.ListTaskRelations(ctx, blocked.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(outgoing) != 1 || outgoing[0].Type != relation.TypeBlocks || outgoing[0].Direction != relation.DirectionOutgoing {
		t.Fatalf("outgoing = %#v", outgoing)
	}
	if len(incoming) != 1 || incoming[0].Type != relation.TypeBlocks || incoming[0].Direction != relation.DirectionIncoming {
		t.Fatalf("incoming = %#v", incoming)
	}
	if err := store.UnlinkTaskRelation(ctx, blocker.ID, blocked.ID, relation.TypeBlocks); err != nil {
		t.Fatal(err)
	}
}

func TestRelationStoreLinksTasksSymmetricallyAndRejectsSelfLink(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	store := NewRelationStore(database)
	taskStore := NewTaskStore(database)
	first, err := taskStore.Create(ctx, task.CreateInput{Title: "前端"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := taskStore.Create(ctx, task.CreateInput{Title: "后端"})
	if err != nil {
		t.Fatal(err)
	}

	if err := store.LinkTasks(ctx, second.ID, first.ID); err != nil {
		t.Fatalf("LinkTasks() error = %v", err)
	}
	for _, ownerID := range []string{first.ID, second.ID} {
		linked, listErr := store.ListTaskRelations(ctx, ownerID)
		if listErr != nil || len(linked) != 1 {
			t.Fatalf("ListTaskRelations(%q) = %#v, %v", ownerID, linked, listErr)
		}
	}
	if err := store.LinkTasks(ctx, first.ID, first.ID); !errors.Is(err, ErrSelfTaskLink) {
		t.Fatalf("LinkTasks(self) error = %v, want ErrSelfTaskLink", err)
	}
	if err := store.UnlinkTasks(ctx, first.ID, second.ID); err != nil {
		t.Fatal(err)
	}
}

func TestRelationStoreRejectsMissingSubjectsAndClosedDatabase(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	store := NewRelationStore(database)
	if err := store.LinkTopicTask(ctx, "missing", "missing"); !errors.Is(err, ErrTopicNotFound) {
		t.Fatalf("missing topic link error = %v", err)
	}
	if err := store.LinkTasksTyped(ctx, "missing", "other", relation.TypeBlocks); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("missing task link error = %v", err)
	}
	if _, err := store.ListTopicTasks(ctx, "missing"); !errors.Is(err, ErrTopicNotFound) {
		t.Fatalf("missing topic list error = %v", err)
	}
	if _, err := store.ListTaskTopics(ctx, "missing"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("missing task topic list error = %v", err)
	}
	if _, err := store.ListTaskRelations(ctx, "missing"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("missing task relation list error = %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	calls := []func() error{
		func() error { return store.LinkTopicTask(ctx, "topic", "task") },
		func() error { return store.UnlinkTopicTask(ctx, "topic", "task") },
		func() error { _, err := store.ListTopicTasks(ctx, "topic"); return err },
		func() error { _, err := store.ListTaskTopics(ctx, "task"); return err },
		func() error { return store.LinkTasks(ctx, "task", "other") },
		func() error { return store.LinkTasksTyped(ctx, "task", "other", relation.TypeBlocks) },
		func() error { return store.UnlinkTasks(ctx, "task", "other") },
		func() error { return store.UnlinkTaskRelation(ctx, "task", "other", relation.TypeBlocks) },
		func() error { _, err := store.ListTaskRelations(ctx, "task"); return err },
	}
	for index, call := range calls {
		if err := call(); err == nil || strings.TrimSpace(err.Error()) == "" {
			t.Fatalf("closed database call %d error = %v", index, err)
		}
	}
}
