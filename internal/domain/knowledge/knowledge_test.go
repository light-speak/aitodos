package knowledge

import "testing"

func TestDecisionInputRequiresExactlyOneSubject(t *testing.T) {
	valid := DecisionInput{TopicID: " topic-1 ", Title: " 采用 SQLite ", Content: " 本地优先 "}.Normalized()
	if err := valid.Validate(); err != nil || valid.TopicID != "topic-1" {
		t.Fatalf("valid decision = %#v, %v", valid, err)
	}
	for _, input := range []DecisionInput{
		{Title: "无主体", Content: "内容"},
		{TopicID: "topic", TaskID: "task", Title: "双主体", Content: "内容"},
	} {
		if err := input.Normalized().Validate(); err == nil {
			t.Fatalf("Validate(%#v) succeeded", input)
		}
	}
}

func TestLabelAndCISnapshotInputValidation(t *testing.T) {
	label := (LabelInput{Name: " bug "}).Normalized()
	if label.Color != "#64748B" || label.Name != "bug" || label.Validate() != nil {
		t.Fatalf("normalized label = %#v", label)
	}
	if err := (LabelInput{Name: "bug", Color: "red"}).Validate(); err == nil {
		t.Fatal("invalid label color succeeded")
	}
	input := (CISnapshotInput{
		Provider: " github ", CommitSHA: " abcdef123 ", State: "passed",
		Checks: []CICheck{{Name: " tests ", State: "passed"}},
	}).Normalized()
	if input.Provider != "github" || input.State != "PASSED" || input.Checks[0].Name != "tests" || input.Validate() != nil {
		t.Fatalf("normalized CI input = %#v", input)
	}
	input.Checks[0].State = "BROKEN"
	if err := input.Validate(); err == nil {
		t.Fatal("invalid CI check state succeeded")
	}
}
