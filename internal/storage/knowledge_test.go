package storage

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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

func TestKnowledgeStoreRejectsInvalidAndMissingReferences(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	store := NewKnowledgeStore(database)
	createdTopic, err := NewTopicStore(database).Create(ctx, topic.CreateInput{Title: "知识边界"})
	if err != nil {
		t.Fatal(err)
	}
	createdTask, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "知识任务"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := store.CreateDecision(ctx, knowledge.DecisionInput{}); err == nil {
		t.Fatal("CreateDecision() accepted invalid input")
	}
	if _, err := store.CreateDecision(ctx, knowledge.DecisionInput{
		TopicID: createdTopic.ID, Title: "替代缺失决策", Content: "内容", SupersedesDecisionID: "missing",
	}); !errors.Is(err, ErrDecisionNotFound) {
		t.Fatalf("missing superseded decision error = %v", err)
	}
	first, err := store.CreateDecision(ctx, knowledge.DecisionInput{TopicID: createdTopic.ID, Title: "旧决策", Content: "旧内容"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateDecision(ctx, knowledge.DecisionInput{
		TopicID: createdTopic.ID, Title: "新决策", Content: "新内容", SupersedesDecisionID: first.ID,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateDecision(ctx, knowledge.DecisionInput{
		TopicID: createdTopic.ID, Title: "再次替代", Content: "不允许", SupersedesDecisionID: first.ID,
	}); err == nil || !strings.Contains(err.Error(), "already superseded") {
		t.Fatalf("already superseded error = %v", err)
	}

	if _, err := store.CreateLabel(ctx, knowledge.LabelInput{}); err == nil {
		t.Fatal("CreateLabel() accepted invalid input")
	}
	label, err := store.CreateLabel(ctx, knowledge.LabelInput{Name: "quality"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateLabel(ctx, knowledge.LabelInput{Name: "quality"}); err == nil {
		t.Fatal("CreateLabel() accepted duplicate name")
	}
	if err := store.AttachTopicLabel(ctx, "missing", label.ID); !errors.Is(err, ErrTopicNotFound) {
		t.Fatalf("missing topic attach error = %v", err)
	}
	if err := store.AttachTaskLabel(ctx, createdTask.ID, "missing"); !errors.Is(err, ErrLabelNotFound) {
		t.Fatalf("missing label attach error = %v", err)
	}
	if err := store.DetachTaskLabel(ctx, "missing", label.ID); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("missing task detach error = %v", err)
	}
	if _, err := store.ListTopicDecisions(ctx, "missing", false); !errors.Is(err, ErrTopicNotFound) {
		t.Fatalf("missing topic decisions error = %v", err)
	}
	if _, err := store.ListTaskDecisions(ctx, "missing", false); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("missing task decisions error = %v", err)
	}
	if _, err := store.ListTopicLabels(ctx, "missing"); !errors.Is(err, ErrTopicNotFound) {
		t.Fatalf("missing topic labels error = %v", err)
	}
	if _, err := store.ListTaskLabels(ctx, "missing"); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("missing task labels error = %v", err)
	}

	for _, summary := range []knowledge.RunSummary{
		{},
		{RunID: "run", Status: "SUCCEEDED", Summary: strings.Repeat("长", 4001)},
		{RunID: "run", Status: "SUCCEEDED", Summary: "完成", PassedTests: -1},
	} {
		if err := store.UpsertRunSummary(ctx, summary); err == nil {
			t.Fatalf("UpsertRunSummary(%#v) unexpectedly succeeded", summary)
		}
	}
	if _, err := store.GetRunSummary(ctx, "missing"); !errors.Is(err, ErrRunSummaryNotFound) {
		t.Fatalf("missing run summary error = %v", err)
	}
	if _, err := store.CreateCISnapshot(ctx, createdTask.ID, knowledge.CISnapshotInput{}); err == nil {
		t.Fatal("CreateCISnapshot() accepted invalid input")
	}
	if _, err := store.CreateCISnapshot(ctx, "missing", knowledge.CISnapshotInput{
		Provider: "github", CommitSHA: "abcdef1", State: "PASSED",
	}); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("missing CI task error = %v", err)
	}
	if _, err := store.ListCISnapshots(ctx, "missing", 10); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("missing CI list task error = %v", err)
	}
	for _, limit := range []int{0, 101} {
		if snapshots, err := store.ListCISnapshots(ctx, createdTask.ID, limit); err != nil || len(snapshots) != 0 {
			t.Fatalf("ListCISnapshots(limit=%d) = %#v, %v", limit, snapshots, err)
		}
	}
}

func TestKnowledgeStoreReportsCorruptProjectionAndClosedDatabase(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	createdTask, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "损坏投影"})
	if err != nil {
		t.Fatal(err)
	}
	now := formatTime(time.Now())
	if _, err := database.ExecContext(ctx, `INSERT INTO runs(
id, purpose, task_id, status, profile_revision_id, claim_token_hash, lease_generation,
lease_expires_at, run_nonce, queued_at, claimed_at, created_at, updated_at, subject_version
) VALUES ('run-corrupt-summary', 'IMPLEMENTATION', ?, 'SUCCEEDED', 'profile-implementer-r1', 'hash', 1,
?, 'nonce', ?, ?, ?, ?, 1)`, createdTask.ID, now, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	store := NewKnowledgeStore(database)
	if err := store.UpsertRunSummary(ctx, knowledge.RunSummary{
		RunID: "run-corrupt-summary", Status: "SUCCEEDED", Summary: "完成",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE run_summaries SET created_at = 'invalid' WHERE run_id = 'run-corrupt-summary'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GetRunSummary(ctx, "run-corrupt-summary"); err == nil {
		t.Fatal("GetRunSummary() accepted an invalid timestamp")
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO ci_check_snapshots(
id, task_id, provider, commit_sha, state, checks_json, source_url, observed_at, created_at
) VALUES ('ci-corrupt', ?, 'github', 'abcdef1', 'PASSED', '{}', '', ?, ?)`, createdTask.ID, now, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListCISnapshots(ctx, createdTask.ID, 10); err == nil {
		t.Fatal("ListCISnapshots() accepted non-array checks")
	}

	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	closedStore := NewKnowledgeStore(database)
	closedCalls := []func() error{
		func() error {
			_, err := closedStore.CreateDecision(ctx, knowledge.DecisionInput{TaskID: createdTask.ID, Title: "决策", Content: "内容"})
			return err
		},
		func() error { _, err := closedStore.CreateLabel(ctx, knowledge.LabelInput{Name: "closed"}); return err },
		func() error { _, err := closedStore.ListLabels(ctx); return err },
		func() error { return closedStore.AttachTaskLabel(ctx, createdTask.ID, "label") },
		func() error { return closedStore.DetachTaskLabel(ctx, createdTask.ID, "label") },
		func() error { _, err := closedStore.GetRunSummary(ctx, "run"); return err },
	}
	for index, call := range closedCalls {
		if err := call(); err == nil || errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("closed call %d error = %v", index, err)
		}
	}
}

func TestKnowledgeStoreRejectsCorruptDecisionLabelAndCITimestamps(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	store := NewKnowledgeStore(database)
	createdTopic, err := NewTopicStore(database).Create(ctx, topic.CreateInput{Title: "损坏知识时间"})
	if err != nil {
		t.Fatal(err)
	}
	createdTask, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "损坏 CI 时间"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO decisions(
id, decision_key, topic_id, task_id, title, content, status, supersedes_decision_id, created_at
) VALUES ('decision-corrupt-time', 'DEC-CORRUPT', ?, NULL, '决策', '内容', 'ACTIVE', NULL, 'invalid')`, createdTopic.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListTopicDecisions(ctx, createdTopic.ID, true); err == nil {
		t.Fatal("ListTopicDecisions() accepted an invalid timestamp")
	}
	if _, err := database.ExecContext(ctx, `INSERT INTO labels(id, name, color, created_at) VALUES ('label-corrupt-time', 'corrupt', '#123456', 'invalid')`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListLabels(ctx); err == nil {
		t.Fatal("ListLabels() accepted an invalid timestamp")
	}
	now := formatTime(time.Now().UTC())
	if _, err := database.ExecContext(ctx, `INSERT INTO ci_check_snapshots(
id, task_id, provider, commit_sha, state, checks_json, source_url, observed_at, created_at
) VALUES ('ci-corrupt-observed', ?, 'github', 'abcdef1', 'PASSED', '[]', '', 'invalid', ?)`, createdTask.ID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListCISnapshots(ctx, createdTask.ID, 10); err == nil {
		t.Fatal("ListCISnapshots() accepted an invalid observed time")
	}
	if _, err := database.ExecContext(ctx, `UPDATE ci_check_snapshots SET observed_at = ?, created_at = 'invalid' WHERE id = 'ci-corrupt-observed'`, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ListCISnapshots(ctx, createdTask.ID, 10); err == nil {
		t.Fatal("ListCISnapshots() accepted an invalid creation time")
	}
}
