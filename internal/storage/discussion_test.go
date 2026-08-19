package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/discussion"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/topic"
)

func TestDiscussionStoreAppendsAndListsTopicMessages(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	topicStore := NewTopicStore(database)
	store := NewDiscussionStore(database)
	createdTopic, err := topicStore.Create(ctx, topic.CreateInput{Title: "讨论上下文"})
	if err != nil {
		t.Fatal(err)
	}

	first, err := store.AppendTopicMessage(ctx, createdTopic.ID, discussion.CreateMessageInput{Content: " 第一条消息 "})
	if err != nil {
		t.Fatalf("AppendTopicMessage() error = %v", err)
	}
	second, err := store.AppendTopicMessage(ctx, createdTopic.ID, discussion.CreateMessageInput{Content: "第二条消息"})
	if err != nil {
		t.Fatalf("AppendTopicMessage() second error = %v", err)
	}
	if first.Sequence != 1 || second.Sequence != 2 || first.ThreadID != second.ThreadID {
		t.Fatalf("messages = %#v, %#v", first, second)
	}
	if first.AuthorKind != discussion.AuthorHuman || first.Content != "第一条消息" {
		t.Fatalf("first message = %#v", first)
	}

	messages, err := store.ListTopicMessages(ctx, createdTopic.ID)
	if err != nil {
		t.Fatalf("ListTopicMessages() error = %v", err)
	}
	if len(messages) != 2 || messages[0].ID != first.ID || messages[1].ID != second.ID {
		t.Fatalf("messages = %#v", messages)
	}
}

func TestDiscussionStoreAppendsTaskMessagesWithTaskReferences(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	taskStore := NewTaskStore(database)
	store := NewDiscussionStore(database)
	owner, err := taskStore.Create(ctx, task.CreateInput{Title: "实现讨论"})
	if err != nil {
		t.Fatal(err)
	}
	related, err := taskStore.Create(ctx, task.CreateInput{Title: "补充测试"})
	if err != nil {
		t.Fatal(err)
	}

	created, err := store.AppendTaskMessage(ctx, owner.ID, discussion.CreateMessageInput{
		Content:       "这个修改需要同步测试",
		LinkedTaskIDs: []string{related.ID},
	})
	if err != nil {
		t.Fatalf("AppendTaskMessage() error = %v", err)
	}
	if len(created.LinkedTaskIDs) != 1 || created.LinkedTaskIDs[0] != related.ID {
		t.Fatalf("created linked tasks = %#v", created.LinkedTaskIDs)
	}

	messages, err := store.ListTaskMessages(ctx, owner.ID)
	if err != nil {
		t.Fatalf("ListTaskMessages() error = %v", err)
	}
	if len(messages) != 1 || messages[0].ID != created.ID || len(messages[0].LinkedTaskIDs) != 1 {
		t.Fatalf("messages = %#v", messages)
	}
	if _, err := store.AppendTaskMessage(ctx, "missing", discussion.CreateMessageInput{Content: "消息"}); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("AppendTaskMessage() error = %v, want ErrTaskNotFound", err)
	}
}

func TestDiscussionStoreRejectsMissingLinkedTaskWithoutPartialMessage(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	topicStore := NewTopicStore(database)
	store := NewDiscussionStore(database)
	createdTopic, err := topicStore.Create(ctx, topic.CreateInput{Title: "讨论关联"})
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.AppendTopicMessage(ctx, createdTopic.ID, discussion.CreateMessageInput{
		Content:       "引用不存在的任务",
		LinkedTaskIDs: []string{"missing"},
	})
	if !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("AppendTopicMessage() error = %v, want ErrTaskNotFound", err)
	}
	messages, listErr := store.ListTopicMessages(ctx, createdTopic.ID)
	if listErr != nil || len(messages) != 0 {
		t.Fatalf("messages after rejected append = %#v, %v", messages, listErr)
	}
}

func TestDiscussionStoreReturnsEmptyThreadAndRejectsMissingTopic(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	topicStore := NewTopicStore(database)
	store := NewDiscussionStore(database)
	createdTopic, err := topicStore.Create(ctx, topic.CreateInput{Title: "空讨论"})
	if err != nil {
		t.Fatal(err)
	}

	messages, err := store.ListTopicMessages(ctx, createdTopic.ID)
	if err != nil || len(messages) != 0 {
		t.Fatalf("ListTopicMessages() = %#v, %v", messages, err)
	}
	if _, err := store.AppendTopicMessage(ctx, "missing", discussion.CreateMessageInput{Content: "消息"}); !errors.Is(err, ErrTopicNotFound) {
		t.Fatalf("AppendTopicMessage() error = %v, want ErrTopicNotFound", err)
	}
	if _, err := store.ListTopicMessages(ctx, "missing"); !errors.Is(err, ErrTopicNotFound) {
		t.Fatalf("ListTopicMessages() error = %v, want ErrTopicNotFound", err)
	}
}
