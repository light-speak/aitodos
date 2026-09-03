package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/light-speak/aitodos/internal/domain/search"
	"github.com/light-speak/aitodos/internal/domain/task"
)

const searchBenchmarkTaskCount = 10000

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
