package objective

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestObjectiveOmitsUnknownCompletionTime(t *testing.T) {
	encoded, err := json.Marshal(Objective{ID: "objective-1", Status: StatusActive})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "completed_at") {
		t.Fatalf("active objective encoded unknown completion time: %s", encoded)
	}
}

func TestCreateInputNormalizesAndValidates(t *testing.T) {
	input := CreateInput{
		RootTopicID: " topic-1 ",
		Statement:   " 发布可验证版本 ",
		Scope:       " 仅本地工作流 ",
		Constraints: []string{" 不自动 push ", "", "不自动 push"},
		CompletionCriteria: []string{
			" 所有必需测试通过 ",
			" 已集成到目标分支 ",
		},
	}.Normalized()
	if err := input.Validate(); err != nil {
		t.Fatal(err)
	}
	if input.RootTopicID != "topic-1" || len(input.Constraints) != 1 || input.Constraints[0] != "不自动 push" {
		t.Fatalf("normalized input = %#v", input)
	}
}

func TestCreateInputRejectsMissingCompletionCriteria(t *testing.T) {
	input := CreateInput{RootTopicID: "topic-1", Statement: "长期目标"}
	if err := input.Validate(); err == nil {
		t.Fatal("Validate() accepted an objective without completion criteria")
	}
}

func TestTransitionAllowsOnlyExplicitLifecycleCommands(t *testing.T) {
	tests := []struct {
		status  Status
		command Command
		want    Status
		valid   bool
	}{
		{StatusActive, CommandPause, StatusPaused, true},
		{StatusPaused, CommandResume, StatusActive, true},
		{StatusActive, CommandAchieve, StatusAchieved, true},
		{StatusActive, CommandCancel, StatusCancelled, true},
		{StatusPaused, CommandCancel, StatusCancelled, true},
		{StatusPaused, CommandAchieve, "", false},
		{StatusAchieved, CommandResume, "", false},
		{StatusCancelled, CommandResume, "", false},
	}
	for _, test := range tests {
		t.Run(string(test.status)+"/"+string(test.command), func(t *testing.T) {
			got, err := Transition(test.status, test.command)
			if test.valid && (err != nil || got != test.want) {
				t.Fatalf("Transition() = %q, %v; want %q", got, err, test.want)
			}
			if !test.valid && err == nil {
				t.Fatalf("Transition() = %q; want error", got)
			}
		})
	}
}

func TestCheckpointInputRequiresUniqueKnownCriteria(t *testing.T) {
	input := CheckpointInput{
		Summary: "完成验证",
		Criteria: []CriterionResult{
			{CriterionID: "criterion-1", Status: CriterionSatisfied, Evidence: "go test ./..."},
			{CriterionID: "criterion-1", Status: CriterionUnknown},
		},
		StopReason: StopProgress,
		NextAction: "继续",
	}
	if err := input.Validate(map[string]struct{}{"criterion-1": {}}); err == nil {
		t.Fatal("Validate() accepted duplicate criterion results")
	}
	input.Criteria = []CriterionResult{{CriterionID: "missing", Status: CriterionUnknown}}
	if err := input.Validate(map[string]struct{}{"criterion-1": {}}); err == nil {
		t.Fatal("Validate() accepted an unknown criterion")
	}
}

func TestAllCriteriaSatisfied(t *testing.T) {
	criteria := []Criterion{{ID: "one", Description: "测试"}, {ID: "two", Description: "集成"}}
	if AllCriteriaSatisfied(criteria, []CriterionResult{
		{CriterionID: "one", Status: CriterionSatisfied, Evidence: "test"},
		{CriterionID: "two", Status: CriterionSatisfied, Evidence: "git"},
	}) != true {
		t.Fatal("complete evidence was not accepted")
	}
	if AllCriteriaSatisfied(criteria, []CriterionResult{{CriterionID: "one", Status: CriterionSatisfied}}) {
		t.Fatal("partial evidence was accepted")
	}
}
