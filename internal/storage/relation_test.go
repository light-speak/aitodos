package storage

import (
	"context"
	"errors"
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
