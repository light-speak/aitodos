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
	valid := TestResultInput{Outcome: OutcomePassed, EvidenceKind: EvidenceCommand, Summary: "go test 通过", Command: "go test ./..."}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	invalid := valid
	invalid.Command = ""
	if err := invalid.Validate(); err == nil {
		t.Fatal("command evidence without command error = nil")
	}
}
