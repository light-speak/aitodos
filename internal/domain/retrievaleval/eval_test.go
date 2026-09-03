package retrievaleval

import (
	"testing"

	"github.com/light-speak/aitodos/internal/domain/search"
)

func TestCreateCaseInputNormalizesAndValidates(t *testing.T) {
	input := CreateCaseInput{
		Query:      "  worktree recovery  ",
		Kinds:      []search.Kind{search.KindTask, search.KindTask, search.KindDecision},
		DocumentID: " TASK:task-1 ",
		Note:       "  regression  ",
	}.Normalized()
	if input.Query != "worktree recovery" || input.DocumentID != "TASK:task-1" || input.Note != "regression" {
		t.Fatalf("normalized input = %#v", input)
	}
	if len(input.Kinds) != 2 || input.Kinds[0] != search.KindDecision || input.Kinds[1] != search.KindTask {
		t.Fatalf("normalized kinds = %#v", input.Kinds)
	}
	if err := input.Validate(); err != nil {
		t.Fatal(err)
	}
	input.DocumentID = ""
	if err := input.Validate(); err == nil {
		t.Fatal("expected missing document ID to fail")
	}
}

func TestCalculateMetricsUsesAllRelevantDocumentsAndFirstRelevantRank(t *testing.T) {
	metrics := CalculateMetrics([]CaseRanking{
		{RelevantCount: 2, Ranks: []int{2, 0}},
		{RelevantCount: 1, Ranks: []int{1}},
		{RelevantCount: 1, Ranks: []int{0}},
	})
	if metrics.CaseCount != 3 || metrics.RelevantCount != 4 || metrics.RecalledCount != 2 || metrics.HitCases != 2 {
		t.Fatalf("metrics counts = %#v", metrics)
	}
	if metrics.RecallAtK != 0.5 || metrics.HitAtK != 2.0/3.0 || metrics.MRR != 0.5 {
		t.Fatalf("metrics values = %#v", metrics)
	}
}
