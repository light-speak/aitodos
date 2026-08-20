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
