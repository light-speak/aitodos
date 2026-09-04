package storage

import (
	"context"
	"errors"
	"strings"
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

func TestExperienceStoreRejectsInvalidSubjectsAndCandidateRuns(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	store := NewExperienceStore(database)
	createdTask, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "候选经验边界"})
	if err != nil {
		t.Fatal(err)
	}
	createdTopic, err := NewTopicStore(database).Create(ctx, topic.CreateInput{Title: "候选主题"})
	if err != nil {
		t.Fatal(err)
	}
	valid := experience.Input{
		TaskID: createdTask.ID, Title: "经验", Summary: "摘要", Guidance: "做法", Applicability: "条件",
	}
	if _, err := store.CreateVerified(ctx, experience.Input{}); err == nil {
		t.Fatal("CreateVerified() accepted invalid input")
	}
	missingTask := valid
	missingTask.TaskID = "missing"
	if _, err := store.CreateVerified(ctx, missingTask); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("missing task error = %v", err)
	}
	missingSuperseded := valid
	missingSuperseded.SupersedesExperienceID = "missing"
	if _, err := store.CreateVerified(ctx, missingSuperseded); !errors.Is(err, ErrExperienceNotFound) {
		t.Fatalf("missing superseded experience error = %v", err)
	}

	for name, mutate := range map[string]func(*experience.Input){
		"topic": func(input *experience.Input) {
			input.TopicID, input.TaskID, input.SourceRunID = createdTopic.ID, "", "run"
		},
		"no run":      func(input *experience.Input) { input.SourceRunID = "" },
		"pinned":      func(input *experience.Input) { input.SourceRunID, input.Pinned = "run", true },
		"supersedes":  func(input *experience.Input) { input.SourceRunID, input.SupersedesExperienceID = "run", "old" },
		"missing run": func(input *experience.Input) { input.SourceRunID = "missing" },
	} {
		t.Run(name, func(t *testing.T) {
			input := valid
			mutate(&input)
			if _, err := store.CreateCandidate(ctx, input); !errors.Is(err, ErrExperienceRunSubjectMismatch) {
				t.Fatalf("CreateCandidate() error = %v", err)
			}
		})
	}
	now := formatTime(time.Now())
	if _, err := database.ExecContext(ctx, `INSERT INTO runs(
id, purpose, topic_id, status, profile_revision_id, claim_token_hash, lease_generation,
lease_expires_at, run_nonce, queued_at, claimed_at, created_at, updated_at, subject_version
) VALUES ('run-planning-candidate', 'PLANNING', ?, 'RUNNING', 'profile-planner-r1', 'hash', 1,
?, 'nonce', ?, ?, ?, ?, 1)`, createdTopic.ID, now, now, now, now, now); err != nil {
		t.Fatal(err)
	}
	wrongPurpose := valid
	wrongPurpose.SourceRunID = "run-planning-candidate"
	if _, err := store.CreateCandidate(ctx, wrongPurpose); !errors.Is(err, ErrExperienceRunSubjectMismatch) {
		t.Fatalf("wrong-purpose candidate error = %v", err)
	}
	if _, err := store.Get(ctx, "missing"); !errors.Is(err, ErrExperienceNotFound) {
		t.Fatalf("missing experience error = %v", err)
	}
	if _, err := store.ListTopic(ctx, "missing", false); !errors.Is(err, ErrTopicNotFound) {
		t.Fatalf("missing topic list error = %v", err)
	}
	if _, err := store.ListTask(ctx, "missing", false); !errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("missing task list error = %v", err)
	}
	if _, err := store.ConfirmCandidate(ctx, "missing"); !errors.Is(err, ErrExperienceNotFound) {
		t.Fatalf("missing candidate confirmation error = %v", err)
	}
	if _, err := store.Challenge(ctx, "missing"); !errors.Is(err, ErrExperienceNotFound) {
		t.Fatalf("missing challenge error = %v", err)
	}
}

func TestExperienceRecallValidatesQueryScopeLimitAndIdempotentOutcome(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	createdTask, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "召回边界"})
	if err != nil {
		t.Fatal(err)
	}
	insertExperienceRun(t, database, createdTask.ID, "run-recall-boundary")
	store := NewExperienceStore(database)
	for index := 0; index < 3; index++ {
		if _, err := store.CreateVerified(ctx, experience.Input{
			TaskID: createdTask.ID, Title: "召回边界经验", Summary: "用于检查召回数量限制",
			Guidance: "限制返回数量", Applicability: "召回边界", Pinned: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, query := range []RecallQuery{
		{},
		{RunID: "run", TaskID: createdTask.ID, Text: "文本", Limit: -1},
		{RunID: "run", TaskID: createdTask.ID, Text: "文本", Limit: 11},
		{RunID: "run", TopicID: "topic", TaskID: createdTask.ID, Text: "文本"},
	} {
		if _, err := store.Recall(ctx, query); err == nil {
			t.Fatalf("Recall(%#v) unexpectedly succeeded", query)
		}
	}
	recalled, err := store.Recall(ctx, RecallQuery{
		RunID: "run-recall-boundary", Purpose: run.PurposeImplementation, TaskID: createdTask.ID,
		Text: "召回边界经验", Limit: 1,
	})
	if err != nil || len(recalled) != 1 {
		t.Fatalf("limited recall = %#v, %v", recalled, err)
	}
	if err := store.RecordOutcome(ctx, recalled[0].RecallID, experience.OutcomeIgnored); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordOutcome(ctx, recalled[0].RecallID, experience.OutcomeIgnored); err != nil {
		t.Fatalf("idempotent outcome failed: %v", err)
	}
	loaded, err := store.Get(ctx, recalled[0].Experience.ID)
	if err != nil || loaded.SuccessfulApplications != 0 || loaded.FailedApplications != 0 {
		t.Fatalf("ignored outcome changed counters: %#v, %v", loaded, err)
	}

	topicRecord := experience.Record{TopicID: "topic"}
	linkedRecord := experience.Record{TopicID: "linked"}
	projectRecord := experience.Record{ProjectWide: true}
	if recallScope(topicRecord, RecallQuery{TopicID: "topic"}) != 1 ||
		recallScope(linkedRecord, RecallQuery{TaskID: "task"}) != 0.85 ||
		recallScope(projectRecord, RecallQuery{TaskID: "task"}) != 0.6 {
		t.Fatal("recallScope() returned unexpected scores")
	}
	query := RecallQuery{RunID: " run ", TaskID: " task ", Text: " text ", Now: time.Now().Add(time.Hour)}
	if err := normalizeRecallQuery(&query); err != nil || query.Limit != 5 || query.RunID != "run" || query.Now.Location() != time.UTC {
		t.Fatalf("normalized query = %#v, %v", query, err)
	}
	if estimateExperienceTokens("abcd中文") != 3 {
		t.Fatalf("estimateExperienceTokens() = %d", estimateExperienceTokens("abcd中文"))
	}
}

func TestExperienceStoreReportsCorruptRowsAndClosedDatabase(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	createdTask, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "经验损坏行"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewExperienceStore(database)
	created, err := store.CreateVerified(ctx, experience.Input{
		TaskID: createdTask.ID, Title: "损坏时间", Summary: "摘要", Guidance: "做法", Applicability: "条件",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `UPDATE experience_records SET created_at = 'invalid' WHERE id = ?`, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get(ctx, created.ID); err == nil {
		t.Fatal("Get() accepted invalid timestamps")
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	closed := NewExperienceStore(database)
	calls := []func() error{
		func() error {
			_, err := closed.CreateVerified(ctx, experience.Input{TaskID: createdTask.ID, Title: "经验", Summary: "摘要", Guidance: "做法", Applicability: "条件"})
			return err
		},
		func() error {
			_, err := closed.CreateCandidate(ctx, experience.Input{TaskID: createdTask.ID, SourceRunID: "run", Title: "经验", Summary: "摘要", Guidance: "做法", Applicability: "条件"})
			return err
		},
		func() error { _, err := closed.Get(ctx, created.ID); return err },
		func() error { _, err := closed.SetPinned(ctx, created.ID, true); return err },
		func() error { _, err := closed.ConfirmCandidate(ctx, created.ID); return err },
		func() error { _, err := closed.Challenge(ctx, created.ID); return err },
		func() error {
			_, err := closed.Recall(ctx, RecallQuery{RunID: "run", TaskID: createdTask.ID, Text: "经验"})
			return err
		},
		func() error { return closed.RecordOutcome(ctx, "recall", experience.OutcomeHelpful) },
		func() error { _, err := closed.ListRunRecalls(ctx, "run"); return err },
	}
	for index, call := range calls {
		if err := call(); err == nil || strings.TrimSpace(err.Error()) == "" {
			t.Fatalf("closed call %d error = %v", index, err)
		}
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
