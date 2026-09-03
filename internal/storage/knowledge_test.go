package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/domain/knowledge"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/topic"
)

func TestDecisionStoreSupersedesDecisionWithinSameSubject(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	createdTopic, err := NewTopicStore(database).Create(ctx, topic.CreateInput{Title: "运行策略"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewKnowledgeStore(database)
	first, err := store.CreateDecision(ctx, knowledge.DecisionInput{
		TopicID: createdTopic.ID, Title: "前台运行", Content: "Daemon 只允许前台运行",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateDecision(ctx, knowledge.DecisionInput{
		TopicID: createdTopic.ID, Title: "显式打开页面", Content: "启动时不主动打开浏览器",
		SupersedesDecisionID: first.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ListTopicDecisions(ctx, createdTopic.ID, true)
	if err != nil || len(items) != 2 || items[0].ID != second.ID || items[1].Status != knowledge.DecisionStatusSuperseded {
		t.Fatalf("decisions = %#v, %v", items, err)
	}
	otherTask, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "其他主体"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateDecision(ctx, knowledge.DecisionInput{
		TaskID: otherTask.ID, Title: "错误替代", Content: "不能跨主体替代", SupersedesDecisionID: second.ID,
	}); !errors.Is(err, ErrDecisionSubjectMismatch) {
		t.Fatalf("cross-subject supersede error = %v", err)
	}
}

func TestKnowledgeStoreManagesLabelsAndCISnapshots(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	createdTopic, err := NewTopicStore(database).Create(ctx, topic.CreateInput{Title: "标签主题"})
	if err != nil {
		t.Fatal(err)
	}
	createdTask, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "CI 任务"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewKnowledgeStore(database)
	label, err := store.CreateLabel(ctx, knowledge.LabelInput{Name: "bug", Color: "#DC2626"})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AttachTopicLabel(ctx, createdTopic.ID, label.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.AttachTaskLabel(ctx, createdTask.ID, label.ID); err != nil {
		t.Fatal(err)
	}
	allLabels, err := store.ListLabels(ctx)
	if err != nil || len(allLabels) != 1 {
		t.Fatalf("all labels = %#v, %v", allLabels, err)
	}
	taskLabels, err := store.ListTaskLabels(ctx, createdTask.ID)
	if err != nil || len(taskLabels) != 1 {
		t.Fatalf("task labels = %#v, %v", taskLabels, err)
	}
	topicLabels, err := store.ListTopicLabels(ctx, createdTopic.ID)
	if err != nil || len(topicLabels) != 1 || topicLabels[0].Name != "bug" {
		t.Fatalf("topic labels = %#v, %v", topicLabels, err)
	}
	if err := store.DetachTaskLabel(ctx, createdTask.ID, label.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.DetachTopicLabel(ctx, createdTopic.ID, label.ID); err != nil {
		t.Fatal(err)
	}
	decision, err := store.CreateDecision(ctx, knowledge.DecisionInput{TaskID: createdTask.ID, Title: "CI 必须通过", Content: "合并前所有必需检查通过"})
	if err != nil {
		t.Fatal(err)
	}
	taskDecisions, err := store.ListTaskDecisions(ctx, createdTask.ID, false)
	if err != nil || len(taskDecisions) != 1 || taskDecisions[0].ID != decision.ID {
		t.Fatalf("task decisions = %#v, %v", taskDecisions, err)
	}
	observedAt := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	snapshot, err := store.CreateCISnapshot(ctx, createdTask.ID, knowledge.CISnapshotInput{
		Provider: "github", CommitSHA: "abcdef123456", State: "PASSED", ObservedAt: observedAt,
		Checks: []knowledge.CICheck{{Name: "CI / go", State: "PASSED"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ListCISnapshots(ctx, createdTask.ID, 10)
	if err != nil || len(items) != 1 || items[0].ID != snapshot.ID || !items[0].ObservedAt.Equal(observedAt) {
		t.Fatalf("CI snapshots = %#v, %v", items, err)
	}
}

func TestKnowledgeStoreUpsertsRunSummaryProjection(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	createdTask, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "摘要任务"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO runs(
id, purpose, task_id, status, profile_revision_id, claim_token_hash, lease_generation,
lease_expires_at, run_nonce, queued_at, claimed_at, created_at, updated_at, subject_version
) VALUES ('run-summary', 'IMPLEMENTATION', ?, 'SUCCEEDED', 'profile-implementer-r1', 'hash', 1,
?, 'nonce', ?, ?, ?, ?, 1)`, createdTask.ID, formatTime(time.Now()), formatTime(time.Now()),
		formatTime(time.Now()), formatTime(time.Now()), formatTime(time.Now())); err != nil {
		t.Fatal(err)
	}
	store := NewKnowledgeStore(database)
	want := knowledge.RunSummary{RunID: "run-summary", Status: "SUCCEEDED", Summary: "实现完成", PassedTests: 3}
	if err := store.UpsertRunSummary(ctx, want); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.GetRunSummary(ctx, want.RunID)
	if err != nil || loaded.Summary != want.Summary || loaded.PassedTests != 3 {
		t.Fatalf("run summary = %#v, %v", loaded, err)
	}
}
