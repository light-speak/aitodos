package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/discussion"
	"github.com/light-speak/aitodos/internal/domain/search"
	"github.com/light-speak/aitodos/internal/domain/task"
	"github.com/light-speak/aitodos/internal/domain/topic"
)

const searchBenchmarkTaskCount = 10000
const searchBenchmarkMessageCount = 100000

func BenchmarkSearchStoreTenThousandTasks(b *testing.B) {
	database, err := Open(context.Background(), filepath.Join(b.TempDir(), "state.db"), ProjectMetadata{
		InstanceID:   "search-benchmark",
		Name:         "search-benchmark",
		RepoRoot:     "/repo",
		GitCommonDir: "/repo/.git",
	})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { database.Close() })
	seedSearchBenchmarkTasks(b, NewTaskStore(database))
	store := NewSearchStore(database)

	for _, benchmark := range []struct {
		name  string
		query string
	}{
		{name: "fts-trigram", query: "故障恢复"},
		{name: "short-like-fallback", query: "恢复"},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			input := search.Query{Text: benchmark.query, Kinds: []search.Kind{search.KindTask}, Limit: 20}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				page, searchErr := store.Search(context.Background(), input)
				if searchErr != nil || len(page.Items) == 0 {
					b.Fatalf("Search(%q) = %#v, %v", benchmark.query, page, searchErr)
				}
			}
		})
	}
}

func BenchmarkSearchStoreHundredThousandMessages(b *testing.B) {
	database, err := Open(context.Background(), filepath.Join(b.TempDir(), "state.db"), ProjectMetadata{
		InstanceID:   "message-search-benchmark",
		Name:         "message-search-benchmark",
		RepoRoot:     "/repo",
		GitCommonDir: "/repo/.git",
	})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { database.Close() })
	seedSearchBenchmarkMessages(b, database)
	store := NewSearchStore(database)

	for _, benchmark := range []struct {
		name  string
		query string
	}{
		{name: "fts-trigram", query: "上下文恢复"},
		{name: "short-like-fallback", query: "恢复"},
	} {
		b.Run(benchmark.name, func(b *testing.B) {
			input := search.Query{Text: benchmark.query, Kinds: []search.Kind{search.KindMessage}, Limit: 20}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				page, searchErr := store.Search(context.Background(), input)
				if searchErr != nil || len(page.Items) == 0 {
					b.Fatalf("Search(%q) = %#v, %v", benchmark.query, page, searchErr)
				}
			}
		})
	}
}

func seedSearchBenchmarkTasks(b *testing.B, store *TaskStore) {
	b.Helper()
	for index := range searchBenchmarkTaskCount {
		title := fmt.Sprintf("普通任务 %05d", index)
		description := "用于固定规模搜索性能基线"
		if index%1000 == 0 {
			title = fmt.Sprintf("故障恢复任务 %05d", index)
			description = "验证 Runner 故障恢复与状态收敛"
		}
		if _, err := store.Create(context.Background(), task.CreateInput{Title: title, Description: description}); err != nil {
			b.Fatalf("seed task %d: %v", index, err)
		}
	}
}

func seedSearchBenchmarkMessages(b *testing.B, database *sql.DB) {
	b.Helper()
	created, err := NewTopicStore(database).Create(context.Background(), topic.CreateInput{Title: "消息检索容量基准"})
	if err != nil {
		b.Fatal(err)
	}
	first, err := NewDiscussionStore(database).AppendTopicMessage(context.Background(), created.ID, discussion.CreateMessageInput{
		Content: "上下文恢复消息 000000",
	})
	if err != nil {
		b.Fatal(err)
	}
	transaction, err := database.BeginTx(context.Background(), nil)
	if err != nil {
		b.Fatal(err)
	}
	defer transaction.Rollback()
	statement, err := transaction.PrepareContext(context.Background(), `
INSERT INTO messages(id, thread_id, sequence, author_kind, content, created_at)
VALUES (?, ?, ?, 'HUMAN', ?, '2026-09-04T00:00:00Z')`)
	if err != nil {
		b.Fatal(err)
	}
	for index := 1; index < searchBenchmarkMessageCount; index++ {
		content := fmt.Sprintf("普通讨论消息 %06d", index)
		if index%10000 == 0 {
			content = fmt.Sprintf("上下文恢复消息 %06d", index)
		}
		if _, err := statement.ExecContext(context.Background(),
			fmt.Sprintf("benchmark-message-%06d", index), first.ThreadID, index+1, content,
		); err != nil {
			b.Fatalf("seed message %d: %v", index, err)
		}
	}
	if err := statement.Close(); err != nil {
		b.Fatal(err)
	}
	if err := transaction.Commit(); err != nil {
		b.Fatal(err)
	}
}
