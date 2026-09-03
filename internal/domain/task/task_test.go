package task

import "testing"

func TestCreateInputAcceptsOnlyP0ThroughP3(t *testing.T) {
	for _, priority := range []int{0, 1, 2, 3} {
		if err := (CreateInput{Title: "任务", Priority: priority}).Validate(); err != nil {
			t.Fatalf("priority P%d error = %v", priority, err)
		}
	}
	for _, priority := range []int{-1, 4} {
		if err := (CreateInput{Title: "任务", Priority: priority}).Validate(); err == nil {
			t.Fatalf("priority %d unexpectedly valid", priority)
		}
	}
}

func TestCreateInputRejectsUnknownTitleSource(t *testing.T) {
	input := CreateInput{Title: "任务", Priority: 1, TitleSource: TitleSource("EXTERNAL")}
	if err := input.Validate(); err == nil {
		t.Fatal("unknown title source unexpectedly valid")
	}
}

func TestTaskEditInputsNormalizeAndValidate(t *testing.T) {
	details := UpdateDetailsInput{Description: " 描述 ", AcceptanceCriteria: " 验收 ", Priority: 2}.Normalized()
	if details.Description != "描述" || details.AcceptanceCriteria != "验收" || details.Validate() != nil {
		t.Fatalf("details = %#v", details)
	}
	if err := (UpdateDetailsInput{Priority: 4}).Validate(); err == nil {
		t.Fatal("invalid priority unexpectedly valid")
	}
	branch := UpdateTargetBranchInput{TargetBranch: " feature/test "}.Normalized()
	if branch.TargetBranch != "feature/test" || branch.Validate() != nil || (UpdateTargetBranchInput{}).Validate() == nil {
		t.Fatalf("branch = %#v", branch)
	}
	title := UpdateTitleInput{Title: " 新标题 "}.Normalized()
	if title.Title != "新标题" || title.Validate() != nil || (UpdateTitleInput{}).Validate() == nil {
		t.Fatalf("title = %#v", title)
	}
}
