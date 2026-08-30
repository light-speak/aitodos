package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/discussion"
	"github.com/light-speak/aitodos/internal/domain/plan"
	"github.com/light-speak/aitodos/internal/domain/search"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/topic"
)

func TestSearchStoreDistinguishesCurrentAndHistoricalPlanRevisions(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	topics := NewTopicStore(database)
	plans := NewPlanStore(database)
	createdTopic, err := topics.Create(ctx, topic.CreateInput{Title: "规划检索"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := plans.CreateRevision(ctx, createdTopic.ID, createdTopic.Version, searchPlanInput("旧版队列设计"))
	if err != nil {
		t.Fatal(err)
	}
	rejected, err := plans.RequestChanges(ctx, first.Plan.ID, plan.ReviewInput{
		ExpectedTopicVersion: 2, RevisionID: first.Revision.ID, Comment: "改成稳定游标",
	})
	if err != nil {
		t.Fatal(err)
	}
	currentTopic, err := topics.Get(ctx, createdTopic.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plans.CreateRevision(ctx, createdTopic.ID, currentTopic.Version, searchPlanInput("新版稳定游标")); err != nil {
		t.Fatal(err)
	}
	if rejected.Plan.Status != plan.StatusChangesRequested {
		t.Fatalf("rejected plan = %#v", rejected.Plan)
	}

	current, err := NewSearchStore(database).Search(ctx, search.Query{Text: "旧版队列", OnlyCurrent: true, Limit: 20})
	if err != nil || len(current.Items) != 0 {
		t.Fatalf("current-only results = %#v, %v", current.Items, err)
	}
	history, err := NewSearchStore(database).Search(ctx, search.Query{Text: "旧版队列", Limit: 20})
	if err != nil || len(history.Items) != 1 || history.Items[0].Current {
		t.Fatalf("historical results = %#v, %v", history.Items, err)
	}
}

func searchPlanInput(summary string) plan.RevisionInput {
	return plan.RevisionInput{Summary: summary, Drafts: []plan.TaskDraftInput{{Title: "实现搜索", Priority: 2}}}
}

func TestSearchMigrationBackfillsExistingCanonicalData(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := configure(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL
)`); err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 31; version++ {
		if err := applyMigration(ctx, database, version); err != nil {
			t.Fatal(err)
		}
	}
	if err := upsertProjectMetadata(ctx, database, ProjectMetadata{
		InstanceID: "search-upgrade", Name: "search-upgrade", RepoRoot: "/repo", GitCommonDir: "/repo/.git",
	}); err != nil {
		t.Fatal(err)
	}
	created, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "迁移前已经存在的搜索内容"})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := OpenExisting(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	page, err := NewSearchStore(upgraded).Search(ctx, search.Query{Text: "已经存在", Limit: 20})
	if err != nil || len(page.Items) != 1 || page.Items[0].SourceID != created.ID {
		t.Fatalf("backfilled results = %#v, %v", page.Items, err)
	}
}

func TestSearchOptimizationMigrationUpgradesExistingVersionThirtyTwo(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "state.db")
	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := configure(ctx, database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.ExecContext(ctx, `CREATE TABLE schema_migrations (
    version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL
)`); err != nil {
		t.Fatal(err)
	}
	for version := 1; version <= 32; version++ {
		if err := applyMigration(ctx, database, version); err != nil {
			t.Fatal(err)
		}
	}
	if err := upsertProjectMetadata(ctx, database, ProjectMetadata{
		InstanceID: "search-v32", Name: "search-v32", RepoRoot: "/repo", GitCommonDir: "/repo/.git",
	}); err != nil {
		t.Fatal(err)
	}
	created, err := NewTaskStore(database).Create(ctx, task.CreateInput{Title: "保留 v32 索引"})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}

	upgraded, err := OpenExisting(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var triggerSQL string
	if err := upgraded.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type = 'trigger' AND name = 'search_documents_au'`).Scan(&triggerSQL); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(triggerSQL, "UPDATE OF title, body, stable_key") {
		t.Fatalf("search_documents_au = %q", triggerSQL)
	}
	var splitTriggers int
	if err := upgraded.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master
WHERE type = 'trigger' AND name IN ('search_topics_au_content', 'search_topics_au_metadata', 'search_tasks_au_content', 'search_tasks_au_metadata')`).Scan(&splitTriggers); err != nil {
		t.Fatal(err)
	}
	if splitTriggers != 4 {
		t.Fatalf("split trigger count = %d", splitTriggers)
	}
	page, err := NewSearchStore(upgraded).Search(ctx, search.Query{Text: "v32 索引", Limit: 20})
	if err != nil || len(page.Items) != 1 || page.Items[0].SourceID != created.ID {
		t.Fatalf("upgraded search = %#v, %v", page.Items, err)
	}
}

func TestSearchStoreIndexesCanonicalItemsAndMessagesIncrementally(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	searchStore := NewSearchStore(database)
	topicStore := NewTopicStore(database)
	taskStore := NewTaskStore(database)
	discussions := NewDiscussionStore(database)

	createdTopic, err := topicStore.Create(ctx, topic.CreateInput{
		Title: "讨论项目全文搜索", Description: "使用 SQLite FTS5 建立读取投影",
	})
	if err != nil {
		t.Fatal(err)
	}
	createdTask, err := taskStore.Create(ctx, task.CreateInput{
		Title: "实现 MCP 只读服务", Description: "复用统一检索读取层",
	})
	if err != nil {
		t.Fatal(err)
	}
	message, err := discussions.AppendTopicMessage(ctx, createdTopic.ID, discussion.CreateMessageInput{
		Content: "中文搜索需要支持连续文字片段",
	})
	if err != nil {
		t.Fatal(err)
	}

	page, err := searchStore.Search(ctx, search.Query{Text: "全文搜索", Limit: 20})
	if err != nil {
		t.Fatalf("Search() topic error = %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].SourceID != createdTopic.ID || page.Items[0].Kind != search.KindTopic {
		t.Fatalf("topic results = %#v", page.Items)
	}

	page, err = searchStore.Search(ctx, search.Query{Text: "MCP", Kinds: []search.Kind{search.KindTask}, Limit: 20})
	if err != nil {
		t.Fatalf("Search() task error = %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].SourceID != createdTask.ID {
		t.Fatalf("task results = %#v", page.Items)
	}

	page, err = searchStore.Search(ctx, search.Query{Text: "连续文字", Kinds: []search.Kind{search.KindMessage}, Limit: 20})
	if err != nil {
		t.Fatalf("Search() message error = %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].SourceID != message.ID || page.Items[0].SubjectID != createdTopic.ID {
		t.Fatalf("message results = %#v", page.Items)
	}
}

func TestSearchStoreTracksUpdatesFiltersAndRebuildsProjection(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	searchStore := NewSearchStore(database)
	taskStore := NewTaskStore(database)

	created, err := taskStore.Create(ctx, task.CreateInput{Title: "旧名称", Description: "索引更新验证"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := taskStore.UpdateTitle(ctx, created.ID, created.Version, task.UpdateTitleInput{Title: "新的索引名称"})
	if err != nil {
		t.Fatal(err)
	}

	page, err := searchStore.Search(ctx, search.Query{
		Text: "新的索引", Kinds: []search.Kind{search.KindTask}, Statuses: []string{string(task.StatusReady)}, Limit: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].SourceID != updated.ID || page.Items[0].Status != string(task.StatusReady) {
		t.Fatalf("updated results = %#v", page.Items)
	}

	if _, err := database.ExecContext(ctx, "DELETE FROM search_documents"); err != nil {
		t.Fatal(err)
	}
	empty, err := searchStore.Search(ctx, search.Query{Text: "新的索引", Limit: 20})
	if err != nil || len(empty.Items) != 0 {
		t.Fatalf("empty projection = %#v, %v", empty, err)
	}
	if err := searchStore.Rebuild(ctx); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	rebuilt, err := searchStore.Search(ctx, search.Query{Text: "新的索引", Limit: 20})
	if err != nil || len(rebuilt.Items) != 1 || rebuilt.Items[0].SourceID != updated.ID {
		t.Fatalf("rebuilt results = %#v, %v", rebuilt, err)
	}
}

func TestSearchStoreExcludesArtifactsAndBoundsPagination(t *testing.T) {
	ctx := context.Background()
	database := openTaskTestDatabase(t)
	searchStore := NewSearchStore(database)
	if _, err := database.ExecContext(ctx, `
INSERT INTO artifacts(
    id, kind, original_name, original_media_type, original_relative_path,
    original_size, original_sha256, optimized_media_type, optimized_relative_path,
    optimized_size, optimized_sha256, created_at
) VALUES ('artifact-secret', 'IMAGE', 'SUPERSECRET.png', 'image/png', 'images/original.png',
          1, 'source-sha', 'image/webp', 'images/optimized.webp', 1, 'optimized-sha',
          '2026-08-31T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}

	page, err := searchStore.Search(ctx, search.Query{Text: "SUPERSECRET", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 {
		t.Fatalf("artifact leaked into search = %#v", page.Items)
	}
	if _, err := searchStore.Search(ctx, search.Query{Text: "query", Limit: 51}); err == nil {
		t.Fatal("Search() should reject an excessive limit")
	}
}
