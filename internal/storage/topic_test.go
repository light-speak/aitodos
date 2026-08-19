package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/topic"
)

func TestTopicStoreCreatesListsAndTransitionsTopicAtomically(t *testing.T) {
	ctx := context.Background()
	store := NewTopicStore(openTaskTestDatabase(t))

	created, err := store.Create(ctx, topic.CreateInput{
		Title:       " 需求讨论 ",
		Description: " 明确范围和限制 ",
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if created.Title != "需求讨论" || created.Description != "明确范围和限制" {
		t.Fatalf("created topic = %#v", created)
	}
	if created.Status != topic.StatusOpen || created.Version != 1 || created.ID == "" || created.Key == "" {
		t.Fatalf("created topic identity = %#v", created)
	}

	listed, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != created.ID {
		t.Fatalf("List() = %#v", listed)
	}

	updated, err := store.ApplyCommand(ctx, created.ID, created.Version, topic.CommandRequestClarification)
	if err != nil {
		t.Fatalf("ApplyCommand() error = %v", err)
	}
	if updated.Status != topic.StatusNeedsClarification || updated.Version != 2 {
		t.Fatalf("updated topic = %#v", updated)
	}

	events, err := store.ListEvents(ctx, created.ID)
	if err != nil {
		t.Fatalf("ListEvents() error = %v", err)
	}
	if len(events) != 2 || events[0].Type != topic.EventCreated || events[1].Type != topic.EventStatusChanged {
		t.Fatalf("events = %#v", events)
	}
}

func TestTopicStoreRejectsStaleVersionAndReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	store := NewTopicStore(openTaskTestDatabase(t))
	created, err := store.Create(ctx, topic.CreateInput{Title: "需求"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyCommand(ctx, created.ID, created.Version, topic.CommandClose); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyCommand(ctx, created.ID, created.Version, topic.CommandClose); !errors.Is(err, ErrTopicVersionConflict) {
		t.Fatalf("ApplyCommand() error = %v, want ErrTopicVersionConflict", err)
	}
	if _, err := store.Get(ctx, "missing"); !errors.Is(err, ErrTopicNotFound) {
		t.Fatalf("Get() error = %v, want ErrTopicNotFound", err)
	}
}
