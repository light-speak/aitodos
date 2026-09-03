package quality

import "testing"

func TestEstimateInputValidate(t *testing.T) {
	valid := EstimateInput{Points: 5, RemainingPoints: 3, Confidence: 0.7, Rationale: "还有接口与测试", Source: EstimateHuman}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	for _, invalid := range []EstimateInput{
		{Points: 4, RemainingPoints: 2, Confidence: 0.5, Rationale: "非法点数", Source: EstimateHuman},
		{Points: 5, RemainingPoints: 6, Confidence: 0.5, Rationale: "剩余过大", Source: EstimateHuman},
		{Points: 5, RemainingPoints: 2, Confidence: 1.1, Rationale: "置信度非法", Source: EstimateHuman},
		{Points: 5, RemainingPoints: 2, Confidence: 0.5, Rationale: "没有 Run", Source: EstimateAI},
	} {
		if err := invalid.Validate(); err == nil {
			t.Fatalf("Validate(%#v) error = nil", invalid)
		}
	}
}

func TestTestResultInputValidate(t *testing.T) {
	exitCode := 0
	valid := TestResultInput{
		Outcome: OutcomePassed, EvidenceKind: EvidenceCommand, Summary: "go test 通过",
		Command: "go test ./...", ArtifactRef: "runs/run-1/stdout.log", SourceRunID: "run-1", ExitCode: &exitCode,
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	for name, mutate := range map[string]func(*TestResultInput){
		"command":  func(input *TestResultInput) { input.Command = "" },
		"artifact": func(input *TestResultInput) { input.ArtifactRef = "" },
		"run":      func(input *TestResultInput) { input.SourceRunID = "" },
		"exit":     func(input *TestResultInput) { input.ExitCode = nil },
		"mismatch": func(input *TestResultInput) { code := 1; input.ExitCode = &code },
		"blocked":  func(input *TestResultInput) { input.Outcome = OutcomeBlocked },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := valid
			mutate(&invalid)
			if err := invalid.Validate(); err == nil {
				t.Fatalf("invalid command evidence = %#v", invalid)
			}
		})
	}
}

func TestTestCaseAndResultRejectInvalidEvidence(t *testing.T) {
	validCase := TestCaseInput{Title: " 回归 ", Required: true, CreatedBy: TestCreatorHuman}
	if err := validCase.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, input := range []TestCaseInput{
		{}, {Title: "测试", SortOrder: -1, CreatedBy: TestCreatorHuman},
		{Title: "测试", CreatedBy: TestCreator("UNKNOWN")},
		{Title: "测试", CreatedBy: TestCreatorAgent},
	} {
		if err := input.Validate(); err == nil {
			t.Fatalf("test case %#v unexpectedly valid", input)
		}
	}
	for _, input := range []TestResultInput{
		{Outcome: TestOutcome("UNKNOWN"), EvidenceKind: EvidenceHuman, Summary: "结果"},
		{Outcome: OutcomePassed, EvidenceKind: EvidenceKind("UNKNOWN"), Summary: "结果"},
		{Outcome: OutcomePassed, EvidenceKind: EvidenceHuman},
		{Outcome: OutcomePassed, EvidenceKind: EvidenceAgentReport, Summary: "通过"},
	} {
		if err := input.Validate(); err == nil {
			t.Fatalf("test result %#v unexpectedly valid", input)
		}
	}
}
