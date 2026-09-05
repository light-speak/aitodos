package storage

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/objective"
	"github.com/light-speak/aitodos/internal/domain/search"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/topic"
)

func TestObjectiveStoreCreatesOneActiveObjectiveAndPersistsCheckpoint(t *testing.T) {
	database := openObjectiveTestDatabase(t)
	rootTopic, err := NewTopicStore(database).Create(t.Context(), topic.CreateInput{Title: "发布生产版本"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewObjectiveStore(database)
	created, err := store.Create(t.Context(), objective.CreateInput{
		RootTopicID: rootTopic.ID, Statement: "让项目达到生产可用",
		Constraints:        []string{"不自动 push"},
		CompletionCriteria: []string{"全部测试通过", "变更已集成"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Objective.Status != objective.StatusActive || created.Revision.Revision != 1 || len(created.Revision.CompletionCriteria) != 2 {
		t.Fatalf("created objective = %#v", created)
	}
	checkpoint, err := store.AppendCheckpoint(t.Context(), created.Objective.ID, created.Objective.Version, objective.CheckpointInput{
		Summary: "完成测试",
		Criteria: []objective.CriterionResult{{
			CriterionID: created.Revision.CompletionCriteria[0].ID,
			Status:      objective.CriterionSatisfied, Evidence: "go test ./...",
		}},
		Remaining: []string{"集成"}, StopReason: objective.StopProgress, NextAction: "执行集成",
	})
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Objective.Version != 2 || checkpoint.LatestCheckpoint == nil || checkpoint.Progress.CriteriaSatisfied != 1 {
		t.Fatalf("checkpoint view = %#v", checkpoint)
	}
	loaded, err := store.GetCurrent(t.Context())
	if err != nil || loaded.LatestCheckpoint == nil || loaded.LatestCheckpoint.Sequence != 1 {
		t.Fatalf("GetCurrent() = %#v, %v", loaded, err)
	}

	otherTopic, err := NewTopicStore(database).Create(t.Context(), topic.CreateInput{Title: "另一个目标"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Create(t.Context(), objective.CreateInput{
		RootTopicID: otherTopic.ID, Statement: "并行目标", CompletionCriteria: []string{"完成"},
	})
	if !errors.Is(err, ErrActiveObjectiveExists) {
		t.Fatalf("second Create() error = %v", err)
	}
}

func TestObjectiveSearchProjectionSurvivesRebuild(t *testing.T) {
	database := openObjectiveTestDatabase(t)
	rootTopic, err := NewTopicStore(database).Create(t.Context(), topic.CreateInput{Title: "检索根议题"})
	if err != nil {
		t.Fatal(err)
	}
	objectiveStore := NewObjectiveStore(database)
	created, err := objectiveStore.Create(t.Context(), objective.CreateInput{
		RootTopicID: rootTopic.ID, Statement: "发布生产可用版本", CompletionCriteria: []string{"容量验证通过"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = objectiveStore.AppendCheckpoint(t.Context(), created.Objective.ID, created.Objective.Version, objective.CheckpointInput{
		Summary: "完成容量基线", Remaining: []string{"恢复演练"}, StopReason: objective.StopProgress, NextAction: "执行恢复演练",
	})
	if err != nil {
		t.Fatal(err)
	}
	searchStore := NewSearchStore(database)
	assertObjectiveSearchKinds(t, searchStore)
	if err := searchStore.Rebuild(t.Context()); err != nil {
		t.Fatal(err)
	}
	assertObjectiveSearchKinds(t, searchStore)
}

func assertObjectiveSearchKinds(t *testing.T, store *SearchStore) {
	t.Helper()
	for _, query := range []struct {
		text string
		kind search.Kind
	}{{"生产可用", search.KindObjective}, {"容量基线", search.KindCheckpoint}} {
		page, err := store.Search(t.Context(), search.Query{Text: query.text, Kinds: []search.Kind{query.kind}})
		if err != nil || len(page.Items) != 1 || page.Items[0].Kind != query.kind {
			t.Fatalf("Search(%q) = %#v, %v", query.text, page, err)
		}
	}
}

func TestObjectiveStoreRequiresEvidenceAndSettledTasksBeforeAchievement(t *testing.T) {
	database := openObjectiveTestDatabase(t)
	rootTopic, err := NewTopicStore(database).Create(t.Context(), topic.CreateInput{Title: "完成登录"})
	if err != nil {
		t.Fatal(err)
	}
	created, err := NewObjectiveStore(database).Create(t.Context(), objective.CreateInput{
		RootTopicID: rootTopic.ID, Statement: "交付登录", CompletionCriteria: []string{"验收通过"},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := NewObjectiveStore(database)
	if _, err := store.ApplyCommand(t.Context(), created.Objective.ID, created.Objective.Version, objective.CommandAchieve); !errors.Is(err, ErrObjectiveNotReady) {
		t.Fatalf("Achieve without checkpoint error = %v", err)
	}
	linkedTask, err := NewTaskStore(database).Create(t.Context(), task.CreateInput{Title: "实现登录"})
	if err != nil {
		t.Fatal(err)
	}
	if err := NewRelationStore(database).LinkTopicTask(t.Context(), rootTopic.ID, linkedTask.ID); err != nil {
		t.Fatal(err)
	}
	withEvidence, err := store.AppendCheckpoint(t.Context(), created.Objective.ID, created.Objective.Version, objective.CheckpointInput{
		Summary:    "Agent 声明完成",
		Criteria:   []objective.CriterionResult{{CriterionID: created.Revision.CompletionCriteria[0].ID, Status: objective.CriterionSatisfied, Evidence: "测试记录"}},
		StopReason: objective.StopReadyToComplete,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApplyCommand(t.Context(), created.Objective.ID, withEvidence.Objective.Version, objective.CommandAchieve); !errors.Is(err, ErrObjectiveNotReady) {
		t.Fatalf("Achieve with unfinished Task error = %v", err)
	}
	if _, err := database.ExecContext(context.Background(), "UPDATE tasks SET status = 'ACCEPTED' WHERE id = ?", linkedTask.ID); err != nil {
		t.Fatal(err)
	}
	achieved, err := store.ApplyCommand(t.Context(), created.Objective.ID, withEvidence.Objective.Version, objective.CommandAchieve)
	if err != nil || achieved.Objective.Status != objective.StatusAchieved || achieved.Objective.CompletedAt == nil || achieved.Objective.CompletedAt.IsZero() {
		t.Fatalf("Achieve() = %#v, %v", achieved, err)
	}
}

func TestObjectiveStoreUsesOptimisticVersionForLifecycle(t *testing.T) {
	database := openObjectiveTestDatabase(t)
	rootTopic, err := NewTopicStore(database).Create(t.Context(), topic.CreateInput{Title: "长期维护"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewObjectiveStore(database)
	created, err := store.Create(t.Context(), objective.CreateInput{
		RootTopicID: rootTopic.ID, Statement: "持续维护", CompletionCriteria: []string{"稳定"},
	})
	if err != nil {
		t.Fatal(err)
	}
	paused, err := store.ApplyCommand(t.Context(), created.Objective.ID, created.Objective.Version, objective.CommandPause)
	if err != nil || paused.Objective.Status != objective.StatusPaused {
		t.Fatalf("Pause() = %#v, %v", paused, err)
	}
	if _, err := store.ApplyCommand(t.Context(), created.Objective.ID, created.Objective.Version, objective.CommandResume); !errors.Is(err, ErrObjectiveVersionConflict) {
		t.Fatalf("stale Resume() error = %v", err)
	}
}

func TestObjectiveStoreFindsCurrentObjectiveOnlyThroughItsRootTopic(t *testing.T) {
	database := openObjectiveTestDatabase(t)
	rootTopic, err := NewTopicStore(database).Create(t.Context(), topic.CreateInput{Title: "长期目标根议题"})
	if err != nil {
		t.Fatal(err)
	}
	otherTopic, err := NewTopicStore(database).Create(t.Context(), topic.CreateInput{Title: "无关议题"})
	if err != nil {
		t.Fatal(err)
	}
	linkedTask, err := NewTaskStore(database).Create(t.Context(), task.CreateInput{Title: "目标内任务"})
	if err != nil {
		t.Fatal(err)
	}
	unrelatedTask, err := NewTaskStore(database).Create(t.Context(), task.CreateInput{Title: "无关任务"})
	if err != nil {
		t.Fatal(err)
	}
	if err := NewRelationStore(database).LinkTopicTask(t.Context(), rootTopic.ID, linkedTask.ID); err != nil {
		t.Fatal(err)
	}
	store := NewObjectiveStore(database)
	created, err := store.Create(t.Context(), objective.CreateInput{
		RootTopicID: rootTopic.ID, Statement: "持续推进", CompletionCriteria: []string{"完成"},
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, lookup := range []struct {
		name string
		load func() (objective.View, error)
	}{
		{"root topic", func() (objective.View, error) { return store.GetForTopic(t.Context(), rootTopic.ID) }},
		{"linked task", func() (objective.View, error) { return store.GetForTask(t.Context(), linkedTask.ID) }},
	} {
		t.Run(lookup.name, func(t *testing.T) {
			loaded, err := lookup.load()
			if err != nil || loaded.Objective.ID != created.Objective.ID {
				t.Fatalf("lookup = %#v, %v", loaded, err)
			}
		})
	}
	if _, err := store.GetForTopic(t.Context(), otherTopic.ID); !errors.Is(err, ErrObjectiveNotFound) {
		t.Fatalf("unrelated topic error = %v", err)
	}
	if _, err := store.GetForTask(t.Context(), unrelatedTask.ID); !errors.Is(err, ErrObjectiveNotFound) {
		t.Fatalf("unrelated task error = %v", err)
	}
}

func openObjectiveTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := Open(context.Background(), filepath.Join(t.TempDir(), "state.db"), ProjectMetadata{
		InstanceID: "objective-test", Name: "test", RepoRoot: "/repo", GitCommonDir: "/repo/.git",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}
