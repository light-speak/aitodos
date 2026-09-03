package plan

import "testing"

func TestRevisionInputValidate(t *testing.T) {
	input := RevisionInput{Summary: " 搜索方案 ", Drafts: []TaskDraftInput{{
		Title: " 建立索引 ", Priority: 1,
		TestCases: []TestCaseInput{{Title: " 可以按类型筛选 ", Required: true}},
	}}}
	if err := input.Validate(); err != nil {
		t.Fatal(err)
	}
	normalized := input.Normalized()
	if normalized.Summary != "搜索方案" || normalized.Drafts[0].Title != "建立索引" {
		t.Fatalf("normalized = %#v", normalized)
	}
}

func TestRevisionInputRejectsMissingDrafts(t *testing.T) {
	if err := (RevisionInput{Summary: "方案"}).Validate(); err == nil {
		t.Fatal("Validate() should reject an empty plan")
	}
}

func TestReviewInputRejectsBlankChangesComment(t *testing.T) {
	input := ReviewInput{ExpectedTopicVersion: 2, RevisionID: "revision-1", Comment: "  "}
	if err := input.ValidateChangesRequest(); err == nil {
		t.Fatal("ValidateChangesRequest() should reject a blank comment")
	}
}

func TestPlanningResultNormalizesAndValidatesReply(t *testing.T) {
	result := PlanningResult{Reply: " 继续澄清 ", Plan: &RevisionInput{Summary: " 方案 ", Drafts: []TaskDraftInput{{Title: " Task ", TestCases: []TestCaseInput{{Title: " 测试 "}}}}}}
	if err := result.Validate(); err != nil {
		t.Fatal(err)
	}
	normalized := result.Normalized()
	if normalized.Reply != "继续澄清" || normalized.Plan == nil || normalized.Plan.Drafts[0].TestCases[0].Title != "测试" {
		t.Fatalf("normalized = %#v", normalized)
	}
	if err := (PlanningResult{}).Validate(); err == nil {
		t.Fatal("blank planning reply unexpectedly valid")
	}
}

func TestTaskDraftRejectsInvalidFields(t *testing.T) {
	for _, draft := range []TaskDraftInput{
		{},
		{Title: "Task", Priority: 4},
		{Title: "Task", TestCases: []TestCaseInput{{Title: " "}}},
	} {
		if err := draft.Validate(); err == nil {
			t.Fatalf("draft %#v unexpectedly valid", draft)
		}
	}
}
