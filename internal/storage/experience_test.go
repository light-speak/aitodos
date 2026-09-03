package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/light-speak/aitodos/internal/domain/experience"
	"github.com/light-speak/aitodos/internal/domain/run"
	"github.com/light-speak/aitodos/internal/domain/search"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/topic"
)

func TestExperienceStoreCreatesSupersedesAndSearchesRecords(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	createdTask, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "同步 Git worktree"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewExperienceStore(database)
	first, err := store.CreateVerified(ctx, experience.Input{
		TaskID: createdTask.ID, Title: "先同步目标分支", Summary: "Worktree 过期会造成集成冲突",
		Guidance: "实现前检查目标分支并同步基线", Applicability: "涉及长期 Task worktree 时", ProjectWide: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateVerified(ctx, experience.Input{
		TaskID: createdTask.ID, Title: "同步并验证基线", Summary: "先同步再执行测试",
		Guidance: "同步后记录 base SHA", Applicability: "涉及长期 Task worktree 时",
		SupersedesExperienceID: first.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ListTask(ctx, createdTask.ID, true)
	if err != nil || len(items) != 2 || items[0].ID != second.ID || items[1].Status != experience.StatusSuperseded {
		t.Fatalf("experiences = %#v, %v", items, err)
	}
	page, err := NewSearchStore(database).Search(ctx, search.Query{Text: "worktree", Kinds: []search.Kind{search.KindExperience}, OnlyCurrent: true})
	if err != nil || len(page.Items) != 1 || page.Items[0].SourceID != second.ID {
		t.Fatalf("experience search = %#v, %v", page, err)
	}
	if err := NewSearchStore(database).Rebuild(ctx); err != nil {
		t.Fatal(err)
	}
	page, err = NewSearchStore(database).Search(ctx, search.Query{Text: "worktree", Kinds: []search.Kind{search.KindExperience}, OnlyCurrent: true})
	if err != nil || len(page.Items) != 1 || page.Items[0].SourceID != second.ID {
		t.Fatalf("rebuilt experience search = %#v, %v", page, err)
	}
	pinned, err := store.SetPinned(ctx, second.ID, true)
	if err != nil || !pinned.Pinned {
		t.Fatalf("pinned = %#v, %v", pinned, err)
	}
	challenged, err := store.Challenge(ctx, second.ID)
	if err != nil || challenged.Status != experience.StatusChallenged {
		t.Fatalf("challenged = %#v, %v", challenged, err)
	}
	if _, err := store.Challenge(ctx, second.ID); err == nil {
		t.Fatal("重复标记反例应被拒绝")
	}
	if _, err := store.SetPinned(ctx, "missing", true); !errors.Is(err, ErrExperienceNotFound) {
		t.Fatalf("pin missing error = %v", err)
	}
}

func TestExperienceStoreRecallsRelevantRecordsAndTracksOutcome(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	createdTask, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "修复 Git worktree 同步冲突"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewExperienceStore(database)
	relevant, err := store.CreateVerified(ctx, experience.Input{
		TaskID: createdTask.ID, Title: "Git worktree 同步", Summary: "先更新目标提交再继续",
		Guidance: "检查 ahead/behind", Applicability: "worktree 长期复用", Pinned: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateVerified(ctx, experience.Input{
		TaskID: createdTask.ID, Title: "CSS 配色", Summary: "调整按钮颜色",
		Guidance: "检查对比度", Applicability: "页面样式", ProjectWide: true,
	}); err != nil {
		t.Fatal(err)
	}
	insertExperienceRun(t, database, createdTask.ID, "run-experience")
	recalled, err := store.Recall(ctx, RecallQuery{
		RunID: "run-experience", Purpose: run.PurposeImplementation, TaskID: createdTask.ID,
		Text: "修复 Git worktree 同步冲突", Limit: 5, Now: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(recalled) != 1 || recalled[0].Experience.ID != relevant.ID || recalled[0].Score.Final <= 0 {
		t.Fatalf("recalled = %#v", recalled)
	}
	if err := store.RecordOutcome(ctx, recalled[0].RecallID, experience.OutcomeHelpful); err != nil {
		t.Fatal(err)
	}
	history, err := store.ListRunRecalls(ctx, "run-experience")
	if err != nil || len(history) != 1 || history[0].Outcome != experience.OutcomeHelpful {
		t.Fatalf("recall history = %#v, %v", history, err)
	}
	loaded, err := store.Get(ctx, relevant.ID)
	if err != nil || loaded.RecallCount != 1 || loaded.SuccessfulApplications != 1 {
		t.Fatalf("loaded = %#v, %v", loaded, err)
	}
	if err := store.RecordOutcome(ctx, recalled[0].RecallID, experience.OutcomeHarmful); err != nil {
		t.Fatal(err)
	}
	loaded, err = store.Get(ctx, relevant.ID)
	if err != nil || loaded.SuccessfulApplications != 0 || loaded.FailedApplications != 1 {
		t.Fatalf("reclassified = %#v, %v", loaded, err)
	}
	if err := store.RecordOutcome(ctx, recalled[0].RecallID, experience.Outcome("INVALID")); err == nil {
		t.Fatal("invalid outcome should fail")
	}
	if err := store.RecordOutcome(ctx, "missing", experience.OutcomeHelpful); !errors.Is(err, ErrExperienceRecallNotFound) {
		t.Fatalf("missing recall error = %v", err)
	}
}

func TestExperienceStoreListsTopicAndRejectsCrossSubjectSupersede(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	firstTopic, err := NewTopicStore(database).Create(ctx, topic.CreateInput{Title: "第一主题"})
	if err != nil {
		t.Fatal(err)
	}
	secondTopic, err := NewTopicStore(database).Create(ctx, topic.CreateInput{Title: "第二主题"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewExperienceStore(database)
	created, err := store.CreateVerified(ctx, experience.Input{
		TopicID: firstTopic.ID, Title: "先澄清", Summary: "不确定时先提问",
		Guidance: "提出一个最小问题", Applicability: "需求存在关键歧义",
	})
	if err != nil {
		t.Fatal(err)
	}
	items, err := store.ListTopic(ctx, firstTopic.ID, false)
	if err != nil || len(items) != 1 || items[0].ID != created.ID {
		t.Fatalf("topic experiences = %#v, %v", items, err)
	}
	_, err = store.CreateVerified(ctx, experience.Input{
		TopicID: secondTopic.ID, Title: "错误替代", Summary: "不能跨主体",
		Guidance: "拒绝", Applicability: "任意", SupersedesExperienceID: created.ID,
	})
	if !errors.Is(err, ErrExperienceSubjectMismatch) {
		t.Fatalf("cross subject error = %v", err)
	}
	if _, err := store.CreateVerified(ctx, experience.Input{TopicID: "missing", Title: "缺失", Summary: "缺失", Guidance: "缺失", Applicability: "缺失"}); !errors.Is(err, ErrTopicNotFound) {
		t.Fatalf("missing topic error = %v", err)
	}
}

func TestExperienceStoreCreatesIdempotentCandidateAndConfirmsIt(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	createdTask, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "保留实现经验"})
	if err != nil {
		t.Fatal(err)
	}
	insertExperienceRun(t, database, createdTask.ID, "run-candidate")
	store := NewExperienceStore(database)
	input := experience.Input{
		TaskID: createdTask.ID, SourceRunID: "run-candidate", Title: "迁移后重建索引",
		Summary: "结构变化后重建可恢复的搜索投影", Guidance: "先迁移规范表，再原子重建搜索索引",
		Applicability: "修改 SQLite 搜索投影结构时", ProjectWide: true,
	}
	created, err := store.CreateCandidate(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := store.CreateCandidate(ctx, input)
	if err != nil || duplicate.ID != created.ID {
		t.Fatalf("duplicate candidate = %#v, %v", duplicate, err)
	}
	if created.Status != experience.StatusCandidate || created.VerificationCount != 0 || created.SourceRunID != "run-candidate" {
		t.Fatalf("candidate = %#v", created)
	}
	activeItems, err := store.ListTask(ctx, createdTask.ID, false)
	if err != nil || len(activeItems) != 0 {
		t.Fatalf("candidate entered active list = %#v, %v", activeItems, err)
	}
	active, err := store.ConfirmCandidate(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if active.Status != experience.StatusActive || active.VerificationCount != 1 {
		t.Fatalf("confirmed = %#v", active)
	}
	activeItems, err = store.ListTask(ctx, createdTask.ID, false)
	if err != nil || len(activeItems) != 1 || activeItems[0].ID != created.ID {
		t.Fatalf("confirmed candidate missing from active list = %#v, %v", activeItems, err)
	}
	if _, err := store.ConfirmCandidate(ctx, created.ID); err == nil {
		t.Fatal("重复确认候选应被拒绝")
	}
	otherTask, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "另一个任务"})
	if err != nil {
		t.Fatal(err)
	}
	input.TaskID = otherTask.ID
	if _, err := store.CreateCandidate(ctx, input); !errors.Is(err, ErrExperienceRunSubjectMismatch) {
		t.Fatalf("run subject mismatch error = %v", err)
	}
}

func insertExperienceRun(t *testing.T, database sqlExecutor, taskID, runID string) {
	t.Helper()
	now := formatTime(time.Now().UTC())
	_, err := database.ExecContext(t.Context(), `INSERT INTO runs(
id, purpose, task_id, status, profile_revision_id, claim_token_hash, lease_generation,
lease_expires_at, run_nonce, queued_at, claimed_at, created_at, updated_at, subject_version
) VALUES (?, 'IMPLEMENTATION', ?, 'RUNNING', 'profile-implementer-r1', 'hash', 1, ?, 'nonce', ?, ?, ?, ?, 1)`,
		runID, taskID, now, now, now, now, now)
	if err != nil {
		t.Fatal(err)
	}
}
