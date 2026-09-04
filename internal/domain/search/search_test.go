package search

import (
	"strings"
	"testing"
	"time"
)

func TestQueryNormalizeValidateAndKinds(t *testing.T) {
	query := Query{
		Text: " task ", Kinds: []Kind{" TASK ", KindTask, KindTopic},
		Statuses: []string{" READY ", "READY", ""},
	}
	normalized := query.Normalized()
	if normalized.Text != "task" || normalized.Limit != 20 || len(normalized.Kinds) != 2 || len(normalized.Statuses) != 1 {
		t.Fatalf("normalized = %#v", normalized)
	}
	if err := query.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []Kind{
		KindTopic, KindTask, KindMessage, KindPlanRevision, KindClarification,
		KindDecision, KindRunSummary, KindCICheck, KindLabel, KindExperience,
	} {
		if !kind.Valid() {
			t.Fatalf("kind %s is invalid", kind)
		}
	}
	if Kind("UNKNOWN").Valid() {
		t.Fatal("unknown kind accepted")
	}
}

func TestQueryRejectsInvalidBounds(t *testing.T) {
	now := time.Now()
	cases := []Query{
		{},
		{Text: strings.Repeat("a", 501)},
		{Text: "task", Limit: 51},
		{Text: "task", Kinds: []Kind{"UNKNOWN"}},
		{Text: "task", Kinds: []Kind{KindTask, KindTopic, KindMessage, KindPlanRevision, KindClarification, "EXTRA"}},
		{Text: "task", Statuses: []string{strings.Repeat("x", 101)}},
		{Text: "task", Cursor: strings.Repeat("x", 101)},
		{Text: "task", UpdatedAfter: now, UpdatedBefore: now.Add(-time.Hour)},
	}
	for _, query := range cases {
		if err := query.Validate(); err == nil {
			t.Fatalf("Validate(%#v) succeeded", query)
		}
	}
}
