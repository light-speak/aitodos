package knowledge

import (
	"strings"
	"testing"
)

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

func TestKnowledgeInputValidationBoundaries(t *testing.T) {
	decision := DecisionInput{TaskID: "task", Title: "决策", Content: "内容"}
	for _, mutate := range []func(*DecisionInput){
		func(input *DecisionInput) { input.Title = "" },
		func(input *DecisionInput) { input.Title = strings.Repeat("x", 201) },
		func(input *DecisionInput) { input.Content = "" },
		func(input *DecisionInput) { input.Content = strings.Repeat("x", 20001) },
	} {
		input := decision
		mutate(&input)
		if err := input.Validate(); err == nil {
			t.Fatalf("invalid decision accepted: %#v", input)
		}
	}
	for _, label := range []LabelInput{
		{Name: "", Color: "#123456"},
		{Name: strings.Repeat("x", 101), Color: "#123456"},
	} {
		if err := label.Validate(); err == nil {
			t.Fatalf("invalid label accepted: %#v", label)
		}
	}
	validCI := CISnapshotInput{Provider: "github", CommitSHA: "abcdef1", State: "PASSED"}
	for _, mutate := range []func(*CISnapshotInput){
		func(input *CISnapshotInput) { input.Provider = "" },
		func(input *CISnapshotInput) { input.Provider = strings.Repeat("x", 101) },
		func(input *CISnapshotInput) { input.CommitSHA = "short" },
		func(input *CISnapshotInput) { input.CommitSHA = strings.Repeat("a", 65) },
		func(input *CISnapshotInput) { input.State = "BROKEN" },
		func(input *CISnapshotInput) { input.SourceURL = strings.Repeat("x", 2001) },
		func(input *CISnapshotInput) { input.Checks = []CICheck{{Name: "", State: "PASSED"}} },
		func(input *CISnapshotInput) { input.Checks = []CICheck{{Name: "test", State: "BROKEN"}} },
		func(input *CISnapshotInput) {
			input.Checks = []CICheck{{Name: "test", State: "PASSED", DetailsURL: strings.Repeat("x", 2001)}}
		},
	} {
		input := validCI
		mutate(&input)
		if err := input.Validate(); err == nil {
			t.Fatalf("invalid CI input accepted: %#v", input)
		}
	}
	for _, state := range []string{"PENDING", "PASSED", "FAILED", "CANCELLED", "UNKNOWN"} {
		if !validCIState(state) {
			t.Fatalf("validCIState(%q) = false", state)
		}
	}
}
